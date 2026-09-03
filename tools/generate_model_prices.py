#!/usr/bin/env python3
"""Generate deploy/data/models.json (fork-owned merged model data document).

The merged document is the single repo-maintained source of model + price
data; the app fetches it (with a sha256 anchor) and swaps catalog + price
table atomically. Nothing model/price-related is compiled into the binary.

Document shape:
  {
    "version": 1,
    "models": [ ...catalog: id/aliases/upstream rewrites/lock cards... ],
    "prices": { ...LiteLLM-format price map (USD per token)... }
  }

Sources (priority):
  1. Upstream Wei-Shaw model-price-repo file
     - copy base for every name present upstream (context windows, provider
       metadata, extra cost fields).
  2. Current prices section (fork-owned)
     - authoritative for every catalog name missing upstream (47 synthesized
       names); the fork owns their full entries.
  3. FORK_OVERRIDES table below
     - explicit, machine-checked fork-side values that differ from upstream
       (operator decisions, 2026-08: every divergence resolved to the
       higher price).
  4. Lock cards (models section, lock_price=true)
     - authoritative for their card cost fields; inherited *_priority
       fields rescale by the base-input ratio (upstream convention).

Invariants enforced in --check mode (CI):
  - committed models.json equals generated output;
  - models.json.sha256 matches the committed document;
  - every catalog id/alias has a prices entry.

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
DOC_PATH = REPO / "deploy/data/models.json"
DOC_SHA_PATH = REPO / "deploy/data/models.json.sha256"
UPSTREAM_URL = (
    "https://raw.githubusercontent.com/Wei-Shaw/model-price-repo/"
    "main/model_prices_and_context_window.json"
)

# ---------------------------------------------------------------------------
# Explicit fork-side values that differ from upstream. Every entry here is an
# operator decision (kept or raised to the higher price, 2026-08); the table
# is machine-checked by --check so it can never drift silently.
# Values are USD per token.
# ---------------------------------------------------------------------------
FORK_OVERRIDES: dict[str, dict[str, float]] = {
    # Fork-owned additions (field absent upstream):
    "claude-opus-4-8": {
        "input_cost_per_token_priority": 1e-05,
        "output_cost_per_token_priority": 5e-05,
        "cache_read_input_token_cost_priority": 1e-06,
        "cache_creation_input_token_cost_priority": 1.25e-05,
    },
    "claude-opus-5": {
        "input_cost_per_token_priority": 1e-05,
        "output_cost_per_token_priority": 5e-05,
        "cache_read_input_token_cost_priority": 1e-06,
        "cache_creation_input_token_cost_priority": 1.25e-05,
    },
    "gpt-5.2": {"cache_creation_input_token_cost": 1.75e-06},
    "gpt-5.3-codex": {"cache_creation_input_token_cost": 1.5e-06},
    "gpt-5.3-codex-spark": {"cache_creation_input_token_cost": 1.5e-06},
    "gpt-5.4": {"cache_creation_input_token_cost": 2.5e-06},
    # Operator decision: fork prices higher than upstream list:
    "gpt-5.6": {
        "input_cost_per_token": 5e-06,
        "input_cost_per_token_priority": 1e-05,
        "output_cost_per_token": 3e-05,
        "output_cost_per_token_priority": 6e-05,
        "cache_read_input_token_cost": 5e-07,
        "cache_read_input_token_cost_priority": 1e-06,
        "cache_creation_input_token_cost": 6.25e-06,
        "cache_creation_input_token_cost_priority": 1.25e-05,
    },
    "kimi-k2.5": {"output_cost_per_token": 3e-06},
    "kimi-k2.6": {"output_cost_per_token": 4e-06},
}

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

PRIORITY_FIELDS = {
    "input_cost_per_token_priority",
    "output_cost_per_token_priority",
    "cache_creation_input_token_cost_priority",
    "cache_read_input_token_cost_priority",
}


def infer_provider(name: str, platform: str) -> str:
    """litellm_provider for brand-new synthesized entries, by model ID family."""
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


def load_doc() -> dict:
    doc = json.loads(DOC_PATH.read_text())
    if not isinstance(doc.get("models"), list) or not doc["models"]:
        raise SystemExit("models.json has no models section")
    if not isinstance(doc.get("prices"), dict) or not doc["prices"]:
        raise SystemExit("models.json has no prices section")
    return doc


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
    doc = load_doc()
    models = doc["models"]
    current_prices: dict[str, dict] = doc["prices"]
    by_id = {m["id"]: m for m in models}

    table: dict[str, dict] = {k: dict(v) for k, v in upstream.items()}
    overrides: list[str] = []
    synthesized: list[str] = []

    for model in models:
        name = model["id"]
        price = card_price(model, by_id)
        locked = bool(model.get("lock_price"))
        # 非 lock 模型不带卡是常态（价格由 prices 段承载，上游/仓库生成）；
        # lock 模型必须带卡（卡即该模型的权威价格，CI 强制）。
        if price is None and locked:
            raise SystemExit(f"locked catalog model {name} has no resolvable price card")
        platform = (model.get("platforms") or ["openai"])[0]

        for nm in entry_names(model):
            entry = table.get(nm)
            if entry is None:
                # Catalog name missing upstream: the fork owns the full entry.
                if nm in current_prices:
                    entry = dict(current_prices[nm])
                    table[nm] = entry
                    synthesized.append(nm)
                else:
                    entry = {
                        "mode": "chat",
                        "litellm_provider": infer_provider(nm, platform),
                    }
                    table[nm] = entry
                    synthesized.append(nm)

            base_input = entry.get("input_cost_per_token")

            # 1) Card overrides (lock cards: authoritative for their fields).
            if price is not None:
                for card_field, table_field in COST_FIELDS:
                    if price.get(card_field) is None:
                        continue
                    new = per_token(float(price[card_field]))
                    old = entry.get(table_field)
                    if old is not None and not close(float(old), new):
                        overrides.append(
                            f"{nm}: {table_field} {float(old):.10g} -> {new:.10g} [lock]"
                        )
                    entry[table_field] = new

            # 2) Locked cards: keep the priority tier's base ratio when the
            #    card does not set the priority fields explicitly.
            if price is not None and locked and price.get("input_per_mtok") is not None:
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

            # 3) Explicit fork-side overrides (operator decisions).
            if nm in FORK_OVERRIDES:
                for table_field, value in FORK_OVERRIDES[nm].items():
                    old = entry.get(table_field)
                    if old is not None and not close(float(old), value):
                        overrides.append(
                            f"{nm}: {table_field} {float(old):.10g} -> {value:.10g} [fork]"
                        )
                    entry[table_field] = value

    new_prices = {k: table[k] for k in sorted(table)}
    out_doc = {"version": doc.get("version", 1), "models": models, "prices": new_prices}
    out_bytes = (json.dumps(out_doc, indent=2, ensure_ascii=False, sort_keys=False) + "\n").encode()
    sha = hashlib.sha256(out_bytes).hexdigest()

    if args.check:
        ok = True
        if not DOC_PATH.exists() or DOC_PATH.read_bytes() != out_bytes:
            print(f"[check] FAIL {DOC_PATH.relative_to(REPO)} does not match generated output (regenerate)")
            ok = False
        if not DOC_SHA_PATH.exists() or DOC_SHA_PATH.read_text().strip() != sha:
            print("[check] FAIL stale models.json.sha256 (regenerate)")
            ok = False
        covered = set()
        for model in models:
            covered.update(entry_names(model))
        missing = [n for n in covered if n not in new_prices]
        if missing:
            print(f"[check] FAIL catalog names missing from prices: {sorted(missing)}")
            ok = False
        if ok:
            print(f"[check] OK ({len(new_prices)} price entries, {len(covered)} catalog names covered, sha256 {sha[:12]}...)")
        return 0 if ok else 1

    DOC_PATH.parent.mkdir(parents=True, exist_ok=True)
    DOC_PATH.write_bytes(out_bytes)
    DOC_SHA_PATH.write_text(sha + "\n")

    print(f"price entries:      {len(new_prices)}")
    print(f"catalog names:      {sum(len(entry_names(m)) for m in models)} (all covered)")
    print(f"synthesized (fork-owned, not upstream): {len(synthesized)}")
    for nm in sorted(synthesized):
        print(f"  + {nm}")
    print(f"overridden vs base: {len(overrides)}")
    for line in overrides:
        print(f"  ~ {line}")
    print(f"wrote {DOC_PATH.relative_to(REPO)} (sha256 {sha[:12]}...)")
    print(f"wrote {DOC_SHA_PATH.relative_to(REPO)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
