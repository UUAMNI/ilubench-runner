#!/usr/bin/env python3
"""Run runner.py UNMODIFIED with its hardcoded hosts redirected to the parity mock.

runner.py hardcodes api.anthropic.com, generativelanguage.googleapis.com and the
HuggingFace probe-set URL. To characterize those code paths without keys or
network, this shim imports the module as-is and wraps two functions at runtime:
runner._request_json (every provider call) and urllib.request.urlopen (the
probe-set fetch). Nothing in runner.py changes; the OpenAI-compatible path is
exercised without this shim at all, via --base-url.

Environment:
  ILUBENCH_PARITY_MOCK          e.g. http://127.0.0.1:43123   (required)
  ILUBENCH_PARITY_MOCK_PROFILE  ok | listfail | hf404         (default ok)

Test tooling only.
"""

from __future__ import annotations

import os
import sys
import urllib.request
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import runner  # noqa: E402  (the reference implementation, untouched)

_MOCK = os.environ["ILUBENCH_PARITY_MOCK"].rstrip("/")
_PROFILE = os.environ.get("ILUBENCH_PARITY_MOCK_PROFILE", "ok")
_REWRITES = {
    "https://api.anthropic.com": f"{_MOCK}/{_PROFILE}/anthropic",
    "https://generativelanguage.googleapis.com": f"{_MOCK}/{_PROFILE}/google",
    "https://huggingface.co/datasets/UUAMNI/ilubench/resolve/main": f"{_MOCK}/{_PROFILE}/hf",
}


def _rewrite(url: str) -> str:
    for prefix, target in _REWRITES.items():
        if url.startswith(prefix):
            return target + url[len(prefix):]
    return url


_orig_request_json = runner._request_json


def _request_json(url, headers, payload=None):
    return _orig_request_json(_rewrite(url), headers, payload)


_orig_urlopen = urllib.request.urlopen


def _urlopen(url, *args, **kwargs):
    if isinstance(url, str):
        url = _rewrite(url)
    return _orig_urlopen(url, *args, **kwargs)


runner._request_json = _request_json
urllib.request.urlopen = _urlopen

if __name__ == "__main__":
    sys.argv[0] = "runner.py"  # argparse derives the usage 'prog' from argv[0]
    sys.exit(runner.main())
