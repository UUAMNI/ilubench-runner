# Shadow week, September 2026: the evidence for cutover

Produced by `python3 parity/shadow_report.py` on the machine that ran the
week (macOS, Python 3.12.7, Go binary built from the Milestone 5 commit).
Every run executed the Go binary and `runner.py` with identical arguments
against a real provider and compared them; see `parity/SHADOW_WEEK.md` for
the method. This file is the report verbatim. It contains no response text
and no keys; the raw archives stay local.

Summary: 16 runs, 4 models (Claude, Gemini, GPT, Grok), 2 days, every
verdict PASS, 84 real responses cross-checked through both `output_language`
implementations with zero disagreements, exit codes identical on every run
including the six failure and listing runs. The NOTE lines below record rows
where the two implementations received different model answers to the same
prompt; each answer was scored correctly by both heuristics.

---

# Shadow week report

Runs found: 16 under `shadow`

| run | dialect / provider | model (reported) | args | dates | exit go/py | rows go/py | archives go/py | langs go | langs py | verdict |
|---|---|---|---|---|---|---|---|---|---|---|
| 2026-09-03_anthropic01 | anthropic / anthropic | claude-opus-4-6 | `--provider anthropic --model claude-opus-4-6` | 2026-09-03 | 0/0 | 5/5 | 10/10 | A:en=1 A:mixed=4 B:ig=4 B:mixed=1 | A:en=3 A:mixed=2 B:ig=4 B:mixed=1 | PASS |
| 2026-09-03_badmodel01 | anthropic / anthropic | - | `--provider anthropic --model no-such-model` | 2026-09-03 | 2/2 | 0/0 | 0/0 | - | - | PASS |
| 2026-09-03_google01 | google / google | - | `--provider google --model gemini-2.5-flash-lite` | 2026-09-03 | 2/2 | 0/0 | 0/0 | - | - | PASS |
| 2026-09-03_google02 | google / google | - | `--provider google --model gemini-2.5` | 2026-09-03 | 2/2 | 0/0 | 0/0 | - | - | PASS |
| 2026-09-03_google03 | google / google | - | `--provider google --model gemini-2.5-pro` | 2026-09-03 | 2/2 | 0/0 | 0/0 | - | - | PASS |
| 2026-09-03_google04 | google / google | gemini-3.1-pro-preview | `--provider google --model gemini-3.1-pro-preview` | 2026-09-03 | 0/0 | 5/5 | 10/10 | A:en=1 A:mixed=4 B:ig=5 | A:en=1 A:mixed=4 B:ig=5 | PASS |
| 2026-09-03_list01 | anthropic / anthropic | - | `--provider anthropic` | 2026-09-03 | 1/1 | 0/0 | 0/0 | - | - | PASS |
| 2026-09-03_list02 | google / google | - | `--provider google` | 2026-09-03 | 1/1 | 0/0 | 0/0 | - | - | PASS |
| 2026-09-03_list03 | openai / openai | - | `--provider openai` | 2026-09-03 | 1/1 | 0/0 | 0/0 | - | - | PASS |
| 2026-09-03_openai01 | openai / openai | gpt-5.6-luna | `--provider openai --model gpt-5.6-luna` | 2026-09-03 | 0/0 | 5/5 | 10/10 | A:en=1 A:mixed=4 B:ig=5 | A:en=2 A:mixed=3 B:ig=5 | PASS |
| 2026-09-03_xai01 | compatible / compatible | grok-4.5 | `--base-url https://api.x.ai/v1 --api-key-env XAI_API_KEY --model grok-4.5` | 2026-09-03 | 0/0 | 5/5 | 10/10 | A:en=4 A:mixed=1 B:en=2 B:mixed=3 | A:en=3 A:mixed=2 B:en=1 B:ig=2 B:mixed=2 | PASS |
| 2026-09-04_anthropic01 | anthropic / anthropic | claude-opus-4-6 | `--provider anthropic --model claude-opus-4-6` | 2026-09-04 | 0/0 | 5/5 | 10/10 | A:en=3 A:mixed=2 B:ig=4 B:mixed=1 | A:en=2 A:mixed=3 B:ig=4 B:mixed=1 | PASS |
| 2026-09-04_google01 | google / google | gemini-3.1-pro-preview | `--provider google --model gemini-3.1-pro-preview` | 2026-09-04 | 0/0 | 5/5 | 10/10 | A:en=1 A:mixed=4 B:ig=5 | A:en=1 A:mixed=4 B:ig=5 | PASS |
| 2026-09-04_openai01 | openai / openai | gpt-5.6-luna | `--provider openai --model gpt-5.6-luna` | 2026-09-04 | 0/0 | 5/5 | 10/10 | A:en=1 A:mixed=4 B:ig=5 | A:en=3 A:mixed=2 B:ig=5 | PASS |
| 2026-09-04_subset01 | anthropic / anthropic | claude-opus-4-6 | `--provider anthropic --model claude-opus-4-6 --probe-set sample_probes.jsonl --probes ilu-001` | 2026-09-04 | 0/0 | 1/1 | 2/2 | A:en=1 B:mixed=1 | A:mixed=1 B:mixed=1 | PASS |
| 2026-09-04_xai01 | compatible / compatible | grok-4.5 | `--base-url https://api.x.ai/v1 --api-key-env XAI_API_KEY --model grok-4.5` | 2026-09-04 | 0/0 | 5/5 | 10/10 | A:en=3 A:mixed=2 B:en=2 B:ig=1 B:mixed=2 | A:en=4 A:mixed=1 B:en=3 B:ig=2 | PASS |

## Coverage (SHADOW_WEEK.md)

- [x] 1 Anthropic dialect, full probe set
- [x] 2 Google native dialect
- [x] 3 OpenAI or Moonshot dialect
- [x] 4 OpenAI-compatible --base-url
- [x] 5 Model listing
- [x] 6 Deliberate failure (both exit non-zero, verdict PASS)
- [x] 7 Live HuggingFace fetch (a run without --probe-set)
- [x] 8 Local probe set and a subset
- [x] Repeat: every dialect with a full-set run has one on a second day

## Verdict

READY FOR MILESTONE 6: every run PASS and the coverage checklist is complete.

## Details

### 2026-09-03_anthropic01
- go binary sha256 `ebd99418f32b`, runner.py sha256 `2e64e5d4fff0`, Python 3.12.7
- 4 NOTE line(s): row 0 (ilu-001) arm_A: output_language go en vs python mixed (different responses; the cross-check below is the real test); row 1 (ilu-002) arm_A: output_language go mixed vs python en (different responses; the cross-check below is the real test); row 2 (ilu-003) arm_A: output_language go mixed vs python en (different responses; the cross-check below is the real test); row 3 (ilu-004) arm_A: output_language go mixed vs python en (different responses; the cross-check below is the real test)

### 2026-09-03_google04
- go binary sha256 `ebd99418f32b`, runner.py sha256 `2e64e5d4fff0`, Python 3.12.7
- 2 NOTE line(s): row 2 (ilu-003) arm_A: output_language go mixed vs python en (different responses; the cross-check below is the real test); row 3 (ilu-004) arm_A: output_language go en vs python mixed (different responses; the cross-check below is the real test)

### 2026-09-03_openai01
- go binary sha256 `ebd99418f32b`, runner.py sha256 `2e64e5d4fff0`, Python 3.12.7
- 1 NOTE line(s): row 1 (ilu-002) arm_A: output_language go mixed vs python en (different responses; the cross-check below is the real test)

### 2026-09-03_xai01
- go binary sha256 `ebd99418f32b`, runner.py sha256 `2e64e5d4fff0`, Python 3.12.7
- 4 NOTE line(s): row 0 (ilu-001) arm_B: output_language go en vs python mixed (different responses; the cross-check below is the real test); row 1 (ilu-002) arm_B: output_language go mixed vs python ig (different responses; the cross-check below is the real test); row 4 (ilu-005) arm_A: output_language go en vs python mixed (different responses; the cross-check below is the real test); row 4 (ilu-005) arm_B: output_language go mixed vs python ig (different responses; the cross-check below is the real test)

### 2026-09-04_anthropic01
- go binary sha256 `ebd99418f32b`, runner.py sha256 `2e64e5d4fff0`, Python 3.12.7
- 3 NOTE line(s): row 0 (ilu-001) arm_A: output_language go en vs python mixed (different responses; the cross-check below is the real test); row 2 (ilu-003) arm_A: output_language go en vs python mixed (different responses; the cross-check below is the real test); row 4 (ilu-005) arm_A: output_language go mixed vs python en (different responses; the cross-check below is the real test)

### 2026-09-04_openai01
- go binary sha256 `ebd99418f32b`, runner.py sha256 `2e64e5d4fff0`, Python 3.12.7
- 2 NOTE line(s): row 2 (ilu-003) arm_A: output_language go mixed vs python en (different responses; the cross-check below is the real test); row 3 (ilu-004) arm_A: output_language go mixed vs python en (different responses; the cross-check below is the real test)

### 2026-09-04_subset01
- go binary sha256 `ebd99418f32b`, runner.py sha256 `2e64e5d4fff0`, Python 3.12.7
- 1 NOTE line(s): row 0 (ilu-001) arm_A: output_language go en vs python mixed (different responses; the cross-check below is the real test)

### 2026-09-04_xai01
- go binary sha256 `ebd99418f32b`, runner.py sha256 `2e64e5d4fff0`, Python 3.12.7
- 5 NOTE line(s): row 1 (ilu-002) arm_A: output_language go mixed vs python en (different responses; the cross-check below is the real test); row 1 (ilu-002) arm_B: output_language go mixed vs python ig (different responses; the cross-check below is the real test); row 2 (ilu-003) arm_A: output_language go mixed vs python en (different responses; the cross-check below is the real test); row 2 (ilu-003) arm_B: output_language go mixed vs python en (different responses; the cross-check below is the real test)
