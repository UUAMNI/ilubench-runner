"""Characterization scenarios for runner.py.

Every row of PORT_PLAN.md section A5 that is observable without real keys has a
scenario here. Each scenario is one invocation of the runner in a fresh working
directory; the harness records exit code, stdout, stderr, every file written,
and every HTTP request the mock received.

Placeholders in args/env: {REPO} repo root, {MOCK} mock base URL, {WORK} the
scenario's working directory.

Fields:
  args     command-line arguments
  env      extra environment variables (all *_API_KEY are cleared first)
  shim     route the hardcoded Anthropic/Google/HuggingFace hosts through the
           mock (Python: parity/py_shim.py; Go: in-process endpoint injection)
  profile  mock profile: ok | listfail | hf404
  pre      directories to create in the working directory before the run
  stdout   exact   byte-exact after masking and tail normalization (default)
           prefix  the implementation's stdout must start with the golden
           skip    not compared (usage text differs by design)
  stderr   empty             must be empty (default)
           usage             must be non-empty and mention "error"
           python_traceback  Python must print a traceback; other
                             implementations must print nothing
  note     why the scenario exists, or which deviation it documents
  milestone  PORT_PLAN.md milestone at which the Go port must pass it (default by name)
"""

SAMPLE = "{REPO}/examples/sample_probes.jsonl"
EDGE = "{REPO}/parity/probes/edge.jsonl"
PROBES = "{REPO}/parity/probes"

KEY_OPENAI = {"OPENAI_API_KEY": "sk-parity-openai-0001"}
KEY_ANTHROPIC = {"ANTHROPIC_API_KEY": "sk-ant-parity-0001"}
KEY_GOOGLE = {"GEMINI_API_KEY": "AIza-parity-0001"}

COMPAT = ["--base-url", "{MOCK}/ok/v1", "--model", "mock-gpt", "--probe-set", EDGE]


def S(name, args, env=None, shim=False, profile="ok", pre=(), stdout="exact", stderr="empty", note="",
      milestone=None):
    # Milestone (PORT_PLAN.md B2) at which the Go port must pass this scenario:
    # 2 = CLI, probe loading, dry run, model listing; 3 = probe execution.
    if milestone is None:
        milestone = 3 if name.startswith("run_") or name == "hf_probe_set_run" else 2
    return {"name": name, "args": list(args), "env": dict(env or {}), "shim": shim,
            "profile": profile, "pre": list(pre), "stdout": stdout, "stderr": stderr, "note": note,
            "milestone": milestone}


SCENARIOS = [
    # --- argument parsing (A5 row 7) ---------------------------------------
    S("cli_no_args", [], stderr="usage",
      note="--provider required: exit 2, usage on stderr, nothing on stdout"),
    S("cli_bad_provider", ["--provider", "foo"], stderr="usage"),
    S("cli_compatible_without_base_url", ["--provider", "compatible", "--dry-run"], stderr="usage"),
    S("cli_help", ["-h"], stdout="skip", note="usage text differs by design; exit 0 is the contract"),

    # --- probe set loading (A5 rows 11, 12) --------------------------------
    S("probes_file_missing", ["--provider", "openai", "--probe-set", "nope.jsonl"]),
    S("probes_file_is_directory", ["--provider", "openai", "--probe-set", "{REPO}/examples"],
      note="A5 row 12: misleading 'could not fetch from HuggingFace' message, preserved"),
    S("probes_bad_json", ["--provider", "openai", "--probe-set", f"{PROBES}/bad_json.jsonl"],
      note="tail of 'ERROR: bad probe set:' is Python's JSON error text; prefix-compared"),
    S("probes_missing_field", ["--provider", "openai", "--probe-set", f"{PROBES}/missing_field.jsonl"]),
    S("probes_blank_file", ["--provider", "openai", "--probe-set", f"{PROBES}/blank.jsonl"]),
    S("probes_unknown_ids", ["--provider", "openai", "--probe-set", SAMPLE,
                             "--probes", "ilu-001,ilu-999,nope", "--dry-run"],
      note="Python list repr of the missing ids and of the available ids"),

    # --- dry runs (A5 rows 10, 16, 19, 20) ---------------------------------
    S("dry_run_anthropic_no_key", ["--provider", "anthropic", "--probe-set", SAMPLE, "--dry-run"]),
    S("dry_run_with_key_subset", ["--provider", "google", "--probe-set", SAMPLE,
                                  "--probes", " ilu-003 , ,", "--dry-run"], env=KEY_GOOGLE,
      note="--probes trimming and blank entries"),
    S("dry_run_custom_key_env", ["--base-url", "{MOCK}/ok/v1", "--api-key-env", "MY_ROUTER_KEY",
                                 "--probe-set", SAMPLE, "--dry-run"], env={"MY_ROUTER_KEY": "rk-1"},
      note="provider inferred as 'compatible' from --base-url"),
    S("dry_run_base_url_with_anthropic", ["--provider", "anthropic", "--base-url", "{MOCK}/ok/v1",
                                          "--probe-set", SAMPLE, "--dry-run"],
      note="A5 row 16: base_url printed as endpoint although anthropic calls ignore it"),
    S("dry_run_probes_flag_blank", ["--provider", "openai", "--probe-set", SAMPLE, "--probes", "",
                                    "--dry-run"], note="--probes '' means all probes"),
    S("dry_run_dup_ids_in_file", ["--provider", "openai", "--probe-set", f"{PROBES}/dup_ids.jsonl",
                                  "--dry-run"], note="duplicate id: first position, last content"),
    S("no_key_openai", ["--provider", "openai", "--probe-set", SAMPLE],
      note="A5 row 20: plan prints, then the key check fails with exit 1"),

    # --- model listing (A5 row 15) ------------------------------------------
    S("list_models_compatible", ["--base-url", "{MOCK}/ok/v1/", "--probe-set", SAMPLE], env=KEY_OPENAI,
      note="trailing slash on --base-url is stripped; ids sorted by code point"),
    S("list_models_model_empty_string", ["--base-url", "{MOCK}/ok/v1", "--model", "", "--probe-set", SAMPLE],
      env=KEY_OPENAI, note="--model '' behaves as unset"),
    S("list_models_anthropic", ["--provider", "anthropic", "--probe-set", SAMPLE], env=KEY_ANTHROPIC, shim=True),
    S("list_models_google", ["--provider", "google", "--probe-set", SAMPLE], env=KEY_GOOGLE, shim=True,
      note="'models/' prefix stripped"),
    S("list_models_failure_redacts_key", ["--base-url", "{MOCK}/listfail/v1", "--probe-set", SAMPLE],
      env=KEY_OPENAI, note="A5 row 18: the key inside the error body is replaced by [redacted]"),

    # --- full runs, OpenAI-compatible dialect (A5 rows 1-5, 10, 13, 14, 17, 21) ---
    S("run_compatible_sample", ["--base-url", "{MOCK}/ok/v1", "--model", "mock-gpt", "--probe-set", SAMPLE],
      env=KEY_OPENAI),
    S("run_moonshot_sample", ["--provider", "moonshot", "--base-url", "{MOCK}/ok/v1", "--model", "kimi-mock",
                              "--probe-set", SAMPLE], env={"MOONSHOT_API_KEY": "mk-parity-0001"},
      note="--base-url overrides a named openai-dialect provider's endpoint; provider name kept in outputs"),
    S("run_compatible_unauthenticated", ["--base-url", "{MOCK}/ok/v1", "--model", "m", "--probe-set", SAMPLE,
                                         "--probes", "ilu-001"],
      note="'Note:' line, no Authorization header on the wire"),
    S("run_compatible_edge_texts", COMPAT + ["--probes", "edge-empty,edge-long,edge-nomodel"],
      note="A5 rows 2-5: empty/digits text, 90-code-point opening, Unicode whitespace, NFD input, odd floats"),
    S("run_compatible_arm_b_fails", COMPAT + ["--probes", "edge-fail-b,edge-long"],
      note="A5 row 13: arm A raw file stays, no row, exit 2, later probes still run"),
    S("run_compatible_arm_a_fails", COMPAT + ["--probes", "edge-fail-a"],
      note="A5 row 13: arm B is never called (see requests.txt)"),
    S("run_compatible_bad_shape", COMPAT + ["--probes", "edge-badshape"],
      note="A5 row 9: non-HTTP failure; the text after 'FAIL ...:' is Python's exception repr, prefix-compared"),
    S("run_compatible_non_json", COMPAT + ["--probes", "edge-nonjson"], note="A5 row 9, as above"),
    S("run_compatible_duplicate_probes", ["--base-url", "{MOCK}/ok/v1", "--model", "mock-gpt",
                                          "--probe-set", SAMPLE, "--probes", "ilu-001,ilu-001"],
      note="A5 row 10: runs twice, two rows with the same run_id, raw files overwritten"),
    S("run_compatible_paths", ["--base-url", "{MOCK}/ok/v1", "--model", "meta/llama 3:70b",
                               "--probe-set", SAMPLE, "--probes", "ilu-003",
                               "--out", "sub/rows.jsonl", "--raw-dir", "./raw2/"], pre=["sub"],
      note="A5 row 17: slug in filenames, './' and trailing '/' normalized in notes/evidence"),
    S("run_compatible_out_dir_missing", ["--base-url", "{MOCK}/ok/v1", "--model", "mock-gpt",
                                         "--probe-set", SAMPLE, "--probes", "ilu-003",
                                         "--out", "nodir/rows.jsonl"],
      stdout="prefix", stderr="python_traceback",
      note="A5 row 21: Python tracebacks with exit 1 AFTER the API calls ran and raw files were written; Go prints an ERROR line instead"),

    # --- full runs, Anthropic and Google dialects (via shim / injection) ----
    S("run_anthropic_sample", ["--provider", "anthropic", "--model", "claude-mock", "--probe-set", SAMPLE],
      env=KEY_ANTHROPIC, shim=True, note="max_tokens 2048 on the wire; text blocks joined, tool_use skipped"),
    S("run_anthropic_edge", ["--provider", "anthropic", "--model", "claude-mock", "--probe-set", EDGE,
                             "--probes", "edge-fail-b,edge-badshape,edge-nomodel"], env=KEY_ANTHROPIC, shim=True,
      note="badshape yields empty text (no exception) in this dialect; nomodel falls back to the requested id"),
    S("run_google_sample", ["--provider", "google", "--model", "gemini-mock", "--probe-set", SAMPLE],
      env=KEY_GOOGLE, shim=True, note="modelVersion is the reported model; parts joined"),
    S("run_google_edge", ["--provider", "google", "--model", "gemini-mock", "--probe-set", EDGE,
                          "--probes", "edge-fail-a,edge-badshape,edge-nomodel"], env=KEY_GOOGLE, shim=True),

    # --- probe set fetched from the HuggingFace URL (A5 row 19) -------------
    S("hf_probe_set_dry_run", ["--provider", "anthropic", "--dry-run"], shim=True,
      note="no --probe-set: dry run still fetches; source printed as the URL"),
    S("hf_probe_set_unreachable", ["--provider", "anthropic", "--dry-run"], shim=True, profile="hf404",
      note="HTTP error on fetch: 'could not fetch' message, exit 1; tail prefix-compared"),
    S("hf_probe_set_run", ["--base-url", "{MOCK}/ok/v1", "--model", "mock-gpt"], shim=True, env=KEY_OPENAI,
      note="fetched probes drive a full compatible run"),
]

NAMES = [s["name"] for s in SCENARIOS]
assert len(NAMES) == len(set(NAMES)), "duplicate scenario names"
