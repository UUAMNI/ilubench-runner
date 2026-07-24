# ilubench-runner

Reproduce the [IlùBench](https://huggingface.co/datasets/UUAMNI/ilubench) protocol against any model API, with your own keys, in under five minutes.

## What this measures

Ask a model to explain an Igbo proverb **in English**, and it typically answers as an outsider: gloss, literal translation, comparisons to English proverbs. Ask the *same model* the *same question* **in Igbo**, and it often answers from inside the culture: reasoning from other Igbo proverbs, dropping the translation scaffolding, shifting to the hortatory register the form actually carries. IlùBench calls this *cultural register switching*, and this runner executes the measurement: for every probe it sends the English prompt (arm A) and the Igbo prompt (arm B) as two fresh, independent API calls with no system prompt and provider-default sampling, then archives both raw responses for human scoring. The interesting result is not which language the reply comes back in — it is whether the two replies are the same explanation. *Ilù* = proverb (Igbo).

## Quickstart (60 seconds)

Requirements: Python 3.10+. No dependencies — `runner.py` is a single stdlib-only file.

```bash
git clone https://github.com/UUAMNI/ilubench-runner
cd ilubench-runner

# 1. Put a key in the environment (any one of these):
export ANTHROPIC_API_KEY=...   # or OPENAI_API_KEY / GEMINI_API_KEY / MOONSHOT_API_KEY

# 2. See the call plan without spending a cent:
python runner.py --provider anthropic --dry-run

# 3. List the models your key can reach, pick one, run:
python runner.py --provider anthropic
python runner.py --provider anthropic --model <model-id>
```

A full run over the published probe set is 2 API calls per probe. Structured rows append to `runs.jsonl`; complete raw API responses are archived under `runs_raw/` (git-ignored, local evidence only).

**There is no default model.** Which model you point this at *is* the experiment, so the runner makes you choose: pass `--model`, or omit it and the runner lists what your endpoint offers, then exits.

**Any OpenAI-compatible endpoint works** — OpenRouter, Moonshot, a local vLLM server — which means you can run IlùBench on open-weight models:

```bash
python runner.py --base-url https://openrouter.ai/api/v1 --api-key-env OPENROUTER_API_KEY --model <model-id>
python runner.py --base-url http://localhost:8000/v1 --model <model-id>   # local vLLM; no key needed
```

| `--provider` | Endpoint | Key env var (override with `--api-key-env`) |
|---|---|---|
| `anthropic` | api.anthropic.com | `ANTHROPIC_API_KEY` |
| `openai` | api.openai.com | `OPENAI_API_KEY` |
| `google` | generativelanguage.googleapis.com | `GEMINI_API_KEY` |
| `moonshot` | api.moonshot.ai | `MOONSHOT_API_KEY` |
| `compatible` | your `--base-url` | `OPENAI_API_KEY` |

The probe set is fetched from the [dataset on HuggingFace](https://huggingface.co/datasets/UUAMNI/ilubench) at run time, so you always test against the current published probes. Offline, or want to inspect first? `--probe-set examples/sample_probes.jsonl` runs a 2-probe sample that ships with this repo. `--probes ilu-001,ilu-003` selects specific probes.

Keys are read from environment variables only, are never printed, and are never written to any output file.

## The human-scoring boundary

**This runner elicits; it does not judge.** Every rubric axis in the output is stamped as pending:

| Field | Stamped as | Scored by |
|---|---|---|
| `epistemic_frame` | `pending_human_score` | human rater, per [rubric](https://huggingface.co/datasets/UUAMNI/ilubench/blob/main/rubric.md) |
| `anchor_source` | `pending_human_score` | human rater |
| `register_delta` | `pending_human_score` | human rater (pair-level) |
| `reading` | `pending_human_score` | human rater (pair-level) |
| `cultural_correctness` | `pending_native_review` | **native or near-native Igbo speaker** |

The single exception is `output_language` (`en` / `ig` / `mixed`), auto-filled by a deliberately conservative diacritic-and-stopword heuristic — anything genuinely bilingual lands on `mixed`, and the raw text is always archived so the call can be audited.

This boundary is the methodological point of the benchmark, not a missing feature. IlùBench's central claim is that models produce fluent, confident, *divergent* cultural readings — the July 2026 runs found frontier models disagreeing on what the flagship proverb means, in both languages. Using a model (or a keyword script) to score cultural correctness would smuggle the very judgment under test into the measurement. Whether a reading is the one a culturally fluent speaker would give is a question only a culturally fluent speaker can answer, so the scoring layer is human by design, and the cultural-correctness axis specifically requires native or near-native Igbo speakers. Please do not submit rows with machine-filled rubric axes.

## Output schema

One JSONL row per probe × model pair, appended to `runs.jsonl`:

```json
{
  "run_id": "run-2026-07-24-anthropic-<model>-ilu-003",
  "date": "2026-07-24",
  "provider": "anthropic",
  "model": "<exact model id the API reported>",
  "interface": "API",
  "probe_id": "ilu-003",
  "arm_A": {
    "output_language": "en",
    "epistemic_frame": "pending_human_score",
    "anchor_source": "pending_human_score",
    "notes": "API run, auto-captured. ~210 words. Opens: \"...\". Full raw response: runs_raw/..."
  },
  "arm_B": { "...same shape as arm_A..." },
  "register_delta": "pending_human_score",
  "reading": "pending_human_score",
  "cultural_correctness": "pending_native_review",
  "evidence": "runs_raw/2026-07-24_anthropic_<model>_ilu-003_*.json (local archive, not committed)"
}
```

Each arm's complete raw API response — prompt, response text, exact reported model id, UTC timestamp, and the provider's full JSON — is written to `runs_raw/`, which is git-ignored. Keep it: it is your evidence archive for scoring and audit.

## Protocol invariants

If you change any of these, your rows are not comparable to published IlùBench rows:

1. One fresh API call per arm — no shared conversation state.
2. No system prompt.
3. Provider default sampling — no `temperature` or `top_p` overrides. (Anthropic's API requires an output cap; the runner uses `max_tokens: 2048` there.)
4. Both arms of every probe, verbatim — `prompt_en` is arm A, `prompt_ig` is arm B. Never paraphrase a probe.
5. Record the exact model id the API reports, plus the date — register behavior shifts across model versions.

## Contributing

**Run it on a model we haven't tested and open a PR with your rows.** That is the most useful contribution right now: does register switching hold on open-weight models, smaller models, other providers?

1. Run the full probe set against your model.
2. Open a PR adding your `runs.jsonl` rows (rubric axes still `pending_*` — that's expected; scoring happens with the human judge panel).
3. Keep your `runs_raw/` archive locally in case a row needs auditing.

Replications in other languages (Yorùbá, Hausa, Swahili, Arabic, Mandarin...) are an open question the [dataset card](https://huggingface.co/datasets/UUAMNI/ilubench) discusses — if you build a parallel probe set, we want to hear about it.

## Links

- **Dataset, rubric, and findings:** [huggingface.co/datasets/UUAMNI/ilubench](https://huggingface.co/datasets/UUAMNI/ilubench) (CC-BY-4.0)
- **UUAMNI:** [uuamni.com](https://uuamni.com) — African-language preference data, annotated by native speakers, judged on cultural correctness. Igbo first.
- **Contact:** chuma@uuamni.com

## License

- **Code** (`runner.py`, this repo): [MIT](LICENSE).
- **Probe data** (the published probe set fetched from HuggingFace, and `examples/sample_probes.jsonl`): [CC-BY-4.0](https://creativecommons.org/licenses/by/4.0/) — attribute **IlùBench, UUAMNI (Chuma B. Chukwu Jr.), https://huggingface.co/datasets/UUAMNI/ilubench**.

```bibtex
@misc{ilubench2026,
  title   = {Il\`uBench: Cultural Register Switching in Frontier Language Models},
  author  = {Chukwu, Chuma B.},
  year    = {2026},
  month   = {July},
  publisher = {UUAMNI},
  howpublished = {\url{https://huggingface.co/datasets/UUAMNI/ilubench}},
  note    = {v0.1. Protocol, seed probe set, and multi-model evidence, CC-BY-4.0}
}
```
