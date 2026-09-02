#!/usr/bin/env python3
"""Generate deploy/data/model_prices.json (fork-owned LiteLLM-format price table).

Sources (priority):
  1. Catalog baseline price cards (backend/internal/modelcatalog/data/catalog.json)
     - authoritative for EVERY catalog id/alias (CI enforces the invariant).
  2. Upstream Wei-Shaw model-price-repo file
     - discovery baseline for everything else (context windows, provider
       metadata, extra cost fields) and the copy base for overlapping names.

Rules:
  - Every catalog id/alias ends up with an explicit entry.
  - Name present upstream: copy the upstream entry, then overlay the catalog
    card's cost fields (USD per MTok -> USD per token).
  - Name missing upstream: synthesize a minimal entry from the card.
  - Locked cards (lock_price=true) may rescale inherited *_priority fields by
    the same ratio as the base input change (upstream convention: priority
    tier keeps its base ratio), reported in the diff output.
  - Output is sorted by key; the .sha256 file contains the bare hex digest
    (no filename), matching the runtime hash-comparison convention.

Usage:
  python3 tools/generate_model_prices.py [--upstream FILE] [--check]
    --upstream FILE  use a local upstream copy instead of fetching
    --check          CI mode: verify committed files match generated output
"""

from __future__ import annotations

import argparse
import hashlib
import json
import sys
import urllib.request
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
CATALOG_PATH = REPO / "backend/internal/modelcatalog/data/catalog.json"
CATALOG_SHA_PATH = REPO / "backend/internal/modelcatalog/data/catalog.sha256"
OUT_PATH = REPO / "deploy/data/model_prices.json"
OUT_SHA_PATH = REPO / "deploy/data/model_prices.sha256"
UPSTREAM_URL = (
    "https://raw.githubusercontent.com/Wei-Shaw/model-price-repo/"
    "main/model_prices_and_context_window.json"
)

def infer_provider(name: str, platform: str) -> str:
    """litellm_provider for synthesized entries, by model ID family.

    (Catalog `platforms` is the list of sales platforms, not the upstream
    provider, so the ID prefix is the reliable signal.)
    """
    if name.startswith("claude-"):
        return "anthropic"
    if name.startswith("gpt-"):
        return "openai"
    if name.startswith("gemini-") or name == "gemini-pro-agent":
        return "gemini"
    if name.startswith("glm-"):
        return "zhipu"
    if name.startswith("kimi-") or name == "kimi-for-coding":
        return "moonshot"
    if name.startswith("minimax-"):
        return "minimax"
    if name.startswith("doubao-"):
        return "volcengine"
    if name.startswith("grok-"):
        return "xai"
    return {"anthropic": "anthropic", "openai": "openai", "google": "gemini", "antigravity": "gemini", "zai": "zhipu", "kimi": "moonshot", "minimax": "minimax"}.get(
        platform, "openai"
    )

# (catalog card field, LiteLLM table field)
COST_FIELDS = [
    ("input_per_mtok", "input_cost_per_token"),
    ("output_per_mtok", "output_cost_per_token"),
    ("input_priority_per_mtok", "input_cost_per_token_priority"),
    ("output_priority_per_mtok", "output_cost_per_token_priority"),
    ("cache_write_per_mtok", "cache_creation_input_token_cost"),
    ("cache_write_priority_per_mtok", "cache_creation_input_token_cost_priority"),
    ("cache_read_per_mtok", "cache_read_input_token_cost"),
    ("cache_read_priority_per_mtok", "cache_read_input_token_cost_priority"),
]

BASE_FIELDS = {
    "input_cost_per_token",
    "input_cost_per_token_priority",
    "output_cost_per_token",
    "output_cost_per_token_priority",
}
PRIORITY_FIELDS = {
    "input_cost_per_token_priority",
    "output_cost_per_token_priority",
    "cache_creation_input_token_cost_priority",
    "cache_read_input_token_cost_priority",
}


def per_token(mtok: float) -> float:
    # Clean float representation (5 * 1e-6 -> 5e-06, not 4.999...e-06).
    return float(f"{mtok * 1e-6:.12g}")


def close(a: float, b: float) -> bool:
    return abs(a - b) <= max(1e-18, 1e-12 * max(abs(a), abs(b)))


def load_upstream(arg: str | None) -> dict:
    if arg:
        data = json.loads(Path(arg).read_text())
    else:
        raw = urllib.request.urlopen(UPSTREAM_URL, timeout=30).read()
        data = json.loads(raw)
    if not isinstance(data, dict) or not data:
        raise SystemExit("upstream price file is empty or not an object")
    return data


def load_catalog() -> list[dict]:
    doc = json.loads(CATALOG_PATH.read_text())
    models = doc.get("models")
    if not isinstance(models, list) or not models:
        raise SystemExit("catalog.json has no models")
    return models


def card_price(model: dict, by_id: dict) -> dict | None:
    price = model.get("price")
    if price is None and model.get("price_ref"):
        ref = by_id.get(model["price_ref"])
        price = ref.get("price") if ref else None
    return price


def entry_names(model: dict) -> list[str]:
    names = [model["id"]]
    names.extend(a["id"] for a in model.get("aliases", []))
    return names


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--upstream", help="local upstream price file (default: fetch)")
    ap.add_argument("--check", action="store_true", help="CI: verify committed output")
    args = ap.parse_args()

    upstream = load_upstream(args.upstream)
    models = load_catalog()
    by_id = {m["id"]: m for m in models}

    table: dict[str, dict] = {k: dict(v) for k, v in upstream.items()}
    overrides: list[str] = []
    synthesized: list[str] = []
    covered: set[str] = set()

    for model in models:
        name = model["id"]
        price = card_price(model, by_id)
        if price is None:
            raise SystemExit(f"catalog model {name} has no resolvable price card")
        locked = bool(model.get("lock_price"))
        platform = (model.get("platforms") or ["openai"])[0]

        for nm in entry_names(model):
            covered.add(nm)
            entry = table.get(nm)
            if entry is None:
                entry = {
                    "mode": "chat",
                    "litellm_provider": infer_provider(nm, platform),
                }
                synthesized.append(nm)
                table[nm] = entry

            base_input = entry.get("input_cost_per_token")
            for card_field, table_field in COST_FIELDS:
                if price.get(card_field) is None:
                    continue
                new = per_token(float(price[card_field]))
                old = entry.get(table_field)
                if old is not None and not close(float(old), new):
                    overrides.append(
                        f"{nm}: {table_field} {float(old):.10g} -> {new:.10g}"
                        f"{' [lock]' if locked else ''}"
                    )
                entry[table_field] = new

            # Locked cards: keep the priority tier's base ratio when the card
            # does not set the priority fields explicitly.
            if locked and price.get("input_per_mtok") is not None:
                new_base = per_token(float(price["input_per_mtok"]))
                old_base = base_input
                if old_base and not close(float(old_base), new_base):
                    ratio = new_base / float(old_base)
                    for pf in sorted(PRIORITY_FIELDS):
                        if price_field_set := {
                            "input_cost_per_token_priority": "input_priority_per_mtok",
                            "output_cost_per_token_priority": "output_priority_per_mtok",
                            "cache_creation_input_token_cost_priority": "cache_write_priority_per_mtok",
                            "cache_read_input_token_cost_priority": "cache_read_priority_per_mtok",
                        }.get(pf):
                            if price.get(price_field_set) is None and pf in entry:
                                entry[pf] = float(entry[pf]) * ratio
                                overrides.append(
                                    f"{nm}: {pf} rescaled x{ratio:.4f} (lock base change)"
                                )

    out_bytes = (json.dumps(table, indent=2, sort_keys=True, ensure_ascii=False) + "\n").encode()
    sha = hashlib.sha256(out_bytes).hexdigest()

    if args.check:
        ok = True
        for path, expected in ((OUT_PATH, out_bytes), (OUT_SHA_PATH, (sha + "\n").encode())):
            if not path.exists():
                print(f"[check] FAIL missing {path.relative_to(REPO)}")
                ok = False
            elif path.read_bytes() != expected:
                print(f"[check] FAIL stale {path.relative_to(REPO)} (regenerate)")
                ok = False
        cat_sha = hashlib.sha256(CATALOG_PATH.read_bytes()).hexdigest()
        if not CATALOG_SHA_PATH.exists() or CATALOG_SHA_PATH.read_text().strip() != cat_sha:
            print("[check] FAIL stale catalog.sha256 (regenerate)")
            ok = False
        if ok:
            print(f"[check] OK ({len(table)} entries, sha256 {sha[:12]}...)")
        return 0 if ok else 1

    OUT_PATH.parent.mkdir(parents=True, exist_ok=True)
    OUT_PATH.write_bytes(out_bytes)
    OUT_SHA_PATH.write_text(sha + "\n")
    CATALOG_SHA_PATH.write_text(hashlib.sha256(CATALOG_PATH.read_bytes()).hexdigest() + "\n")

    print(f"entries total:      {len(table)}")
    print(f"catalog names:      {len(covered)} (all covered)")
    print(f"synthesized:        {len(synthesized)}")
    for nm in sorted(synthesized):
        print(f"  + {nm}")
    print(f"overridden vs upstream: {len(overrides)}")
    for line in overrides:
        print(f"  ~ {line}")
    print(f"wrote {OUT_PATH.relative_to(REPO)} (sha256 {sha[:12]}...)")
    print(f"wrote {OUT_SHA_PATH.relative_to(REPO)}")
    print(f"wrote {CATALOG_SHA_PATH.relative_to(REPO)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
