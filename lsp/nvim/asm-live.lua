-- asm-live.lua — inline execution state for the asm-lsp language server.
--
-- Calls the server's custom "asm/run" request, which assembles the buffer,
-- runs it in the tinyemu-go x86-64 emulator, and returns the register/flag
-- changes for each source line. Those are rendered as inline virtual text at
-- the end of each line — a live view of what the code actually does.
--
-- Usage (after the asm-lsp client is attached to the buffer):
--   :lua require('asm-live').run()        -- run the whole program
--   :lua require('asm-live').run_to_cursor()  -- stop before the cursor line
--   :lua require('asm-live').clear()
--
-- Suggested keymaps (put in your ftplugin/asm.lua or the LspAttach autocmd):
--   vim.keymap.set('n', '<leader>rr', function() require('asm-live').run() end,           { buffer = true })
--   vim.keymap.set('n', '<leader>rc', function() require('asm-live').run_to_cursor() end,  { buffer = true })
--   vim.keymap.set('n', '<leader>rx', function() require('asm-live').clear() end,          { buffer = true })

local M = {}

local ns = vim.api.nvim_create_namespace("asm_live")

-- find the asm-lsp client attached to a buffer.
local function client_for(bufnr)
  for _, c in ipairs(vim.lsp.get_clients({ bufnr = bufnr, name = "asm-lsp" })) do
    return c
  end
end

function M.clear(bufnr)
  bufnr = bufnr or 0
  vim.api.nvim_buf_clear_namespace(bufnr, ns, 0, -1)
end

-- render paints the asm/run result as inline virtual text.
local function render(bufnr, result)
  M.clear(bufnr)
  for _, line in ipairs(result.lines or {}) do
    if line.text and line.text ~= "" then
      vim.api.nvim_buf_set_extmark(bufnr, ns, line.line, 0, {
        virt_text = { { "  " .. line.text, "Comment" } },
        virt_text_pos = "eol",
      })
    end
  end

  -- mark where execution stopped, and why.
  if result.stop == "reached-line" and result.stopLine and result.stopLine >= 0 then
    vim.api.nvim_buf_set_extmark(bufnr, ns, result.stopLine, 0, {
      virt_text = { { "  ◀ cursor", "DiagnosticHint" } },
      virt_text_pos = "eol",
    })
  end

  local msg = ("asm: %s (%d steps)"):format(result.stop or "?", result.steps or 0)
  local hl = "DiagnosticInfo"
  if result.stop == "fault" or result.stop == "assemble-error" then
    msg = msg .. " — " .. (result.error or "")
    hl = "DiagnosticError"
  elseif result.stop == "max-steps" or result.stop == "ran-outside-program" then
    hl = "DiagnosticWarn"
  end
  vim.notify(msg, vim.log.levels.INFO)
  if result.stop == "fault" or result.stop == "assemble-error" then
    -- surface the fault prominently
    vim.api.nvim_echo({ { msg, hl } }, false, {})
  end
end

local function run(line)
  local bufnr = vim.api.nvim_get_current_buf()
  local client = client_for(bufnr)
  if not client then
    vim.notify("asm-live: no asm-lsp client attached", vim.log.levels.ERROR)
    return
  end
  local params = {
    textDocument = { uri = vim.uri_from_bufnr(bufnr) },
    line = line,
  }
  client:request("asm/run", params, function(err, result)
    if err then
      vim.notify("asm/run: " .. vim.inspect(err), vim.log.levels.ERROR)
      return
    end
    render(bufnr, result)
  end, bufnr)
end

-- run the whole program.
function M.run()
  run(-1)
end

-- run up to (not including) the line under the cursor.
function M.run_to_cursor()
  local row = vim.api.nvim_win_get_cursor(0)[1] - 1 -- 0-based
  run(row)
end

return M
