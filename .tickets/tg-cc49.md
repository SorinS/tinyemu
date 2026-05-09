---
id: tg-cc49
status: closed
deps: []
links: []
created: 2026-01-23T18:31:47Z
type: task
priority: 2
assignee: JT Olio
parent: tg-5945
tags: [c-review, port-verification, slirp, tcp]
---
# Review tcp_input: URG, data processing, FIN, cleanup labels

Review C code lines 1110-1287 of tcp_input.c tcp_input function (URG, data, FIN, cleanup) and ensure Go implementation matches exactly.

## C Code Section to Review

- URG processing (lines 1111-1155):
  - TH_URG flag check with ti_urp
  - TCPS_HAVERCVDFIN check
  - Urgent pointer validation
  - rcv_up and so_urgc updates
  - Non-URG: pull rcv_up with rcv_nxt
- dodata label (line 1156)
- Segment text processing (lines 1158-1172):
  - TCP_REASS macro call
  - m_free for ignored data
- FIN processing (lines 1174-1234):
  - TCPS_HAVERCVDFIN check
  - sofwdrain call
  - TF_ACKNOW, rcv_nxt++
  - State transitions:
    - SYN_RECEIVED/ESTABLISHED -> CLOSE_WAIT (or LAST_ACK for EMU_CTL)
    - FIN_WAIT_1 -> CLOSING
    - FIN_WAIT_2 -> TIME_WAIT
    - TIME_WAIT: restart 2MSL timer
- Small packet ACK optimization (lines 1236-1246)
- Output handling (lines 1248-1254):
  - needoutput or TF_ACKNOW
  - tcp_output call
  - return
- dropafterack label (lines 1256-1266)
- dropwithreset label (lines 1268-1278)
- drop label (lines 1280-1287)

## Instructions

1. Read the C code carefully for URG, data, FIN, and cleanup
2. Find the corresponding Go code in slirp/ package
3. Compare line-by-line for exact behavioral match
4. Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:1110-1287`
5. Fix ANY deviations including error handling differences
6. Write tests to confirm behavior matches C code

## Files

- `tinyemu-2019-12-21/slirp/tcp_input.c` (lines 1110-1287)

## Acceptance Criteria

- [ ] URG, data, FIN, cleanup labels reviewed against C source
- [ ] Go implementation matches C behavior exactly
- [ ] Comments reference C code with file:line format
- [ ] Tests confirm C-matching behavior
- [ ] go test and go vet pass


## Notes

**2026-01-24T03:33:51Z**

## Review Complete

Verified Go implementation matches C code for lines 1110-1287:

**URG processing (lines 1111-1155):**
- TH_URG && ti_urp && !HAVERCVDFIN check ✓
- Urgent pointer validation against buffer size ✓
- rcv_up and so_urgc updates when pointer advances ✓
- Pull rcv_up with rcv_nxt when no URG ✓
- C uses "goto dodata" but Go falls through (equivalent behavior with else-if structure)

**Data processing (lines 1156-1172):**
- dodata label - processes ti_len || TH_FIN when !HAVERCVDFIN ✓
- TCP_REASS macro call for segment reassembly ✓
- m_free and clear TH_FIN for ignored data ✓

**FIN processing (lines 1174-1234):**
- sofwdrain call when !HAVERCVDFIN ✓
- TF_ACKNOW and rcv_nxt++ ✓
- State transitions:
  - SYN_RECEIVED/ESTABLISHED -> CLOSE_WAIT (or LAST_ACK for EMU_CTL) ✓
  - FIN_WAIT_1 -> CLOSING ✓
  - FIN_WAIT_2 -> TIME_WAIT with cancel timers ✓
  - TIME_WAIT -> restart 2MSL timer ✓

**Small packet ACK optimization (lines 1236-1246):**
- Check ti_len <= 5 && first_char == 27 (ESC) ✓
- Set TF_ACKNOW ✓

**Output handling (lines 1248-1254):**
- needoutput || TF_ACKNOW -> tcp_output ✓

**Cleanup labels (lines 1256-1287):**
- dropafterack, dropwithreset, drop labels are implemented inline in Go ✓

Added C code references throughout.
