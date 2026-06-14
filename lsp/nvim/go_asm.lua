-- go_asm.lua — tinyemu-go assembly language server + live emulation.
-- Self-contained: the `go-asm` binary is resolved from PATH (~/.local/bin),
-- and the inline-run rendering lives here, so this does not depend on the repo.
--
-- Enable by adding one line to your init.lua:  require("go_asm")
--
-- Keymaps (normal mode, in an asm buffer):
--   <leader>rr  run the whole buffer        <leader>rc  run to cursor
--   <leader>rx  clear inline state          <leader>rg  full register file (float)
--   K           hover (bytes + decode)

local M = {}

local ns = vim.api.nvim_create_namespace("asm_live")

local function client_for(bufnr)
  return vim.lsp.get_clients({ bufnr = bufnr, name = "go-asm" })[1]
end

local function clear(bufnr)
  vim.api.nvim_buf_clear_namespace(bufnr or 0, ns, 0, -1)
end

-- nonzero renders the non-zero registers of a {name,value} list as "rax=0x1a6d …".
local function nonzero(regs)
  local parts = {}
  for _, r in ipairs(regs or {}) do
    if r.value and r.value ~= 0 then
      parts[#parts + 1] = ("%s=0x%x"):format(r.name, r.value)
    end
  end
  return table.concat(parts, "  ")
end

local function render(bufnr, result)
  clear(bufnr)
  vim.b[bufnr].asm_last = result -- stash for the register float

  -- per-line changes (the "live" inline view)
  local lastLine = -1
  for _, line in ipairs(result.lines or {}) do
    if line.line > lastLine then lastLine = line.line end
    if line.text and line.text ~= "" then
      vim.api.nvim_buf_set_extmark(bufnr, ns, line.line, 0, {
        virt_text = { { "  " .. line.text, "Comment" } },
        virt_text_pos = "eol",
      })
    end
  end

  -- run-to-cursor marker
  if result.stop == "reached-line" and result.stopLine and result.stopLine >= 0 then
    vim.api.nvim_buf_set_extmark(bufnr, ns, result.stopLine, 0, {
      virt_text = { { "  ◀ cursor", "DiagnosticHint" } },
      virt_text_pos = "eol",
    })
  end

  -- final register state, anchored under the last executed line
  if lastLine >= 0 then
    local fin = nonzero(result.final)
    if fin ~= "" then
      vim.api.nvim_buf_set_extmark(bufnr, ns, lastLine, 0, {
        virt_lines = { { { "  ⇒ " .. fin, "String" } } },
      })
    end
  end

  local lvl = vim.log.levels.INFO
  if result.stop == "fault" or result.stop == "assemble-error" then
    lvl = vim.log.levels.ERROR
  elseif result.stop == "max-steps" or result.stop == "ran-outside-program" then
    lvl = vim.log.levels.WARN
  end
  local msg = ("asm[%s%d]: %s (%d steps)"):format(result.arch or "?", result.bits or 0, result.stop or "?", result.steps or 0)
  if result.error and result.error ~= "" then msg = msg .. " — " .. result.error end
  vim.notify(msg, lvl)
end

local function run(line)
  local bufnr = vim.api.nvim_get_current_buf()
  local client = client_for(bufnr)
  if not client then
    vim.notify("go-asm: no client attached to this buffer", vim.log.levels.ERROR)
    return
  end
  client:request("asm/run", {
    textDocument = { uri = vim.uri_from_bufnr(bufnr) },
    line = line,
  }, function(err, result)
    if err then
      vim.notify("asm/run: " .. vim.inspect(err), vim.log.levels.ERROR)
    elseif result then
      render(bufnr, result)
    end
  end, bufnr)
end

function M.run() run(-1) end
function M.run_to_cursor() run(vim.api.nvim_win_get_cursor(0)[1] - 1) end
function M.clear() clear(0) end

-- registers opens a float with the full final register file from the last run.
function M.registers()
  local res = vim.b[vim.api.nvim_get_current_buf()].asm_last
  if not res or not res.final then
    vim.notify("asm: run the buffer first (<leader>rr)", vim.log.levels.WARN)
    return
  end
  local lines = { ("registers — %s%d, %s"):format(res.arch or "?", res.bits or 0, res.stop or "?") }
  for _, r in ipairs(res.final) do
    lines[#lines + 1] = ("  %-5s 0x%016x  (%d)"):format(r.name, r.value, r.value)
  end
  local buf = vim.api.nvim_create_buf(false, true)
  vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
  vim.bo[buf].modifiable = false
  local w = 0
  for _, l in ipairs(lines) do w = math.max(w, #l) end
  local win = vim.api.nvim_open_win(buf, false, {
    relative = "cursor", row = 1, col = 0, width = w + 1, height = #lines,
    style = "minimal", border = "rounded",
  })
  local function close()
    if vim.api.nvim_win_is_valid(win) then vim.api.nvim_win_close(win, true) end
  end
  -- close when the cursor moves / you leave the buffer (like a hover popup)…
  vim.api.nvim_create_autocmd(
    { "CursorMoved", "CursorMovedI", "InsertEnter", "BufLeave", "WinScrolled" },
    { buffer = vim.api.nvim_get_current_buf(), once = true, callback = close }
  )
  -- …or press q / <Esc> if you focus into it (e.g. via <C-w>w).
  vim.keymap.set("n", "q", close, { buffer = buf, nowait = true })
  vim.keymap.set("n", "<Esc>", close, { buffer = buf, nowait = true })
end

-- ---------------------------------------------------------------------------
-- Stepping debugger (asm/debug/*)
-- ---------------------------------------------------------------------------

local ns_bp = vim.api.nvim_create_namespace("asm_bp") -- breakpoint signs
local breakpoints = {} -- bufnr -> { [line0]=true }
local dbg_line = {} -- bufnr -> last current line (for anchoring the final state)

local function bps_for(bufnr)
  breakpoints[bufnr] = breakpoints[bufnr] or {}
  return breakpoints[bufnr]
end

local function redraw_breakpoints(bufnr)
  vim.api.nvim_buf_clear_namespace(bufnr, ns_bp, 0, -1)
  for line, on in pairs(bps_for(bufnr)) do
    if on then
      vim.api.nvim_buf_set_extmark(bufnr, ns_bp, line, 0,
        { sign_text = "●", sign_hl_group = "DiagnosticError" })
    end
  end
end

function M.toggle_breakpoint()
  local bufnr = vim.api.nvim_get_current_buf()
  local line = vim.api.nvim_win_get_cursor(0)[1] - 1
  local set = bps_for(bufnr)
  set[line] = (not set[line]) or nil
  redraw_breakpoints(bufnr)
end

local function render_debug(bufnr, st)
  clear(bufnr) -- wipes run/debug overlay (ns); breakpoints (ns_bp) persist
  vim.b[bufnr].asm_last = st
  if st.stop == "assemble-error" then
    vim.notify("go-asm debug: " .. (st.error or "assemble error"), vim.log.levels.ERROR)
    return
  end

  local anchor = st.line
  if st.line and st.line >= 0 then
    dbg_line[bufnr] = st.line
    vim.api.nvim_buf_set_extmark(bufnr, ns, st.line, 0, {
      line_hl_group = "Visual",
      virt_text = { { "  ▶ step " .. (st.steps or 0), "DiagnosticInfo" } },
      virt_text_pos = "eol",
    })
    local regs = nonzero(st.regs)
    if regs ~= "" then
      vim.api.nvim_buf_set_extmark(bufnr, ns, st.line, 0,
        { virt_lines = { { { "    " .. regs, "Comment" } } } })
    end
    pcall(vim.api.nvim_win_set_cursor, 0, { st.line + 1, 0 })
  else
    anchor = dbg_line[bufnr] -- run ended: anchor final state at the last line
    if anchor then
      local regs = nonzero(st.regs)
      if regs ~= "" then
        vim.api.nvim_buf_set_extmark(bufnr, ns, anchor, 0,
          { virt_lines = { { { "  ⇒ " .. regs, "String" } } } })
      end
    end
  end

  local lvl = (st.stop == "fault") and vim.log.levels.ERROR or vim.log.levels.INFO
  local tail = (st.stop ~= "" and st.stop) or ("at line " .. ((st.line or 0) + 1))
  vim.notify(("debug[%s%d]: %s (step %d)"):format(st.arch or "?", st.bits or 0, tail, st.steps or 0), lvl)
end

local function dbg(method, extra)
  local bufnr = vim.api.nvim_get_current_buf()
  local client = client_for(bufnr)
  if not client then
    vim.notify("go-asm: no client attached to this buffer", vim.log.levels.ERROR)
    return
  end
  local params = { textDocument = { uri = vim.uri_from_bufnr(bufnr) } }
  for k, v in pairs(extra or {}) do params[k] = v end
  client:request(method, params, function(err, state)
    if err then
      vim.notify(method .. ": " .. vim.inspect(err), vim.log.levels.ERROR)
    elseif state then
      render_debug(bufnr, state)
    end
  end, bufnr)
end

function M.dbg_step() dbg("asm/debug/step") end

function M.dbg_continue()
  local bufnr = vim.api.nvim_get_current_buf()
  local list = {}
  for line, on in pairs(bps_for(bufnr)) do
    if on then list[#list + 1] = line end
  end
  dbg("asm/debug/continue", { breakpoints = list })
end

function M.dbg_stop()
  local bufnr = vim.api.nvim_get_current_buf()
  local client = client_for(bufnr)
  if client then
    client:request("asm/debug/stop",
      { textDocument = { uri = vim.uri_from_bufnr(bufnr) } }, function() end, bufnr)
  end
  dbg_line[bufnr] = nil
  clear(bufnr)
end

-- One-time setup: filetype, server start, keymaps.
if not vim.g.go_asm_loaded then
  vim.g.go_asm_loaded = true

  vim.filetype.add({ extension = { asm = "asm", nasm = "nasm" } })

  vim.api.nvim_create_autocmd("FileType", {
    pattern = { "asm", "nasm" },
    callback = function(args)
      vim.lsp.start({
        name = "go-asm",
        cmd = { "go-asm" }, -- resolved from PATH
        root_dir = vim.fs.dirname(args.file),
      })
    end,
  })

  vim.api.nvim_create_autocmd("LspAttach", {
    callback = function(args)
      local c = vim.lsp.get_client_by_id(args.data.client_id)
      if c and c.name == "go-asm" then
        local o = { buffer = args.buf }
        -- live run
        vim.keymap.set("n", "<leader>rr", M.run, o)            -- run buffer
        vim.keymap.set("n", "<leader>rc", M.run_to_cursor, o)  -- run to cursor
        vim.keymap.set("n", "<leader>rx", M.clear, o)          -- clear overlay
        vim.keymap.set("n", "<leader>rg", M.registers, o)      -- register float
        -- stepping debugger
        vim.keymap.set("n", "<leader>rs", M.dbg_step, o)         -- step (arms on first press)
        vim.keymap.set("n", "<leader>rn", M.dbg_continue, o)     -- continue to breakpoint/end
        vim.keymap.set("n", "<leader>rb", M.toggle_breakpoint, o) -- toggle breakpoint
        vim.keymap.set("n", "<leader>rq", M.dbg_stop, o)         -- quit debug session
      end
    end,
  })
end

return M
