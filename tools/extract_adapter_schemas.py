#!/usr/bin/env python3
"""
Extract per-adapter, per-pillar sub-criterion labels from the adapter MDs
stored in /var/lib/ft/ft.db (sector_scorecards-style or crypto_adapters table).

Output: JS const D25_SCHEMA_BY_ADAPTER mapping adapter slug → {Q1..Q9: {label, subs}}.

Heuristic: For each adapter MD, find the section starting with "### Q<n>"
through the next "### Q<n+1>" or "###" boundary. Within that section, find
the first markdown table whose first column header contains "Sub-criterion".
Extract first-column values from data rows; strip leading/trailing **bold**
markdown.
"""
import sqlite3
import re
import sys
import json

DB_PATH = sys.argv[1] if len(sys.argv) > 1 else "/var/lib/ft/ft.db"
ALT18_ADAPTERS = ["l1", "l2", "defi", "infra", "depin", "rwa", "speculative"]
PILLARS = ["Q1", "Q2", "Q3", "Q4", "Q5", "Q6", "Q7", "Q8", "Q9"]
# BTC monetary_12 uses M1-M6 per Six-Pillar Monetary-Asset Scorecard
# (BTC adapter MD §3). BTC v1 fixture stores {"M1":..,"M6":..}.
BTC_PILLARS = ["M1", "M2", "M3", "M4", "M5", "M6"]
BTC_PILLAR_LABELS = {
    "M1": "Cycle Phase",
    "M2": "Macro Regime",
    "M3": "Flows",
    "M4": "Network Health",
    "M5": "Sentiment",
    "M6": "Technical & Structural Regime",
}

# Q7 in most adapters is a single "highest catalyst score" pillar with no
# multi-row sub-criterion table; fall back to generic single-sub label.
Q7_FALLBACK = ["Highest-scoring catalyst within window"]

# Default pillar labels (used when extractor finds a section but the header
# text is just "Q1 — ...", not a useful display label).
PILLAR_LABELS = {
    "Q1": "Bottleneck / Narrative State",
    "Q2": "Tokenomics",
    "Q3": "Moat / Network Effect",
    "Q4": "Adoption Intensity",
    "Q5": "Value Accrual",
    "Q6": "Security & Decentralization",
    "Q7": "Catalyst",
    "Q8": "Technicals & Regime Alignment",
    "Q9": "Team & Founder Risk",
}


def strip_md(s: str) -> str:
    # Strip surrounding **bold** and *italic*
    s = s.strip()
    s = re.sub(r"^\*\*(.*?)\*\*$", r"\1", s)
    s = re.sub(r"^\*(.*?)\*$", r"\1", s)
    return s.strip()


def extract_pillar_label(section: str) -> str | None:
    # First line is the heading, e.g. "### Q1 — Bottleneck / Narrative State *(sector-specialized)*"
    first_line = section.split("\n", 1)[0]
    m = re.match(r"^###\s+Q\d+\s*[—\-]\s*(.+?)(?:\s*\*\(.*\)\*)?\s*$", first_line)
    if not m:
        return None
    return m.group(1).strip()


def extract_subs_from_section(section: str) -> list[str]:
    """
    Find the first markdown table where the first header column contains
    'Sub-criterion'. Return the first-column values from each data row.
    """
    lines = section.splitlines()
    in_table = False
    sub_col_idx = 0
    rows = []
    for i, ln in enumerate(lines):
        if not in_table:
            # Look for a header row starting with '|'
            if ln.strip().startswith("|") and "Sub-criterion" in ln:
                in_table = True
                # Next line should be the separator '| --- | --- |'
                continue
            continue
        # In-table: separator OR data row
        stripped = ln.strip()
        if not stripped.startswith("|"):
            # Table ended
            break
        if re.match(r"^\|\s*[-:]+\s*\|", stripped):
            # Separator row
            continue
        # Data row: split by |
        cells = [c.strip() for c in stripped.strip("|").split("|")]
        if len(cells) < 2:
            continue
        label = strip_md(cells[0])
        # Skip empty rows
        if not label:
            continue
        # Skip rows that are clearly not sub-criteria (e.g., header labels
        # repeated, "Pillar score" annotations)
        if label.lower().startswith("pillar score"):
            continue
        rows.append(label)
    return rows


def split_into_pillar_sections(md: str, prefix: str = "Q") -> dict[str, str]:
    """
    Return a dict { 'Q1': <section text>, ... } (or 'M1'.. if prefix='M')
    by splitting on '### <prefix>n —' headings.
    """
    parts = re.split(rf"(?=^###\s+{prefix}\d+\s)", md, flags=re.MULTILINE)
    out: dict[str, str] = {}
    for p in parts:
        m = re.match(rf"^###\s+{prefix}(\d+)\s", p)
        if not m:
            continue
        n = int(m.group(1))
        if 1 <= n <= 9:
            out[f"{prefix}{n}"] = p
    return out


def extract_pillar_label_for_prefix(section: str, prefix: str) -> str | None:
    """Same as extract_pillar_label but accepts 'M' prefix as well as 'Q'."""
    first_line = section.split("\n", 1)[0]
    m = re.match(rf"^###\s+{prefix}\d+\s*[—\-]\s*(.+?)(?:\s*\*\(.*\)\*)?\s*$", first_line)
    if not m:
        return None
    return m.group(1).strip()


def main():
    conn = sqlite3.connect(DB_PATH)
    cur = conn.cursor()
    out = {}
    for slug in ALT18_ADAPTERS:
        row = cur.execute(
            "SELECT markdown_current FROM crypto_adapters WHERE slug = ?", (slug,)
        ).fetchone()
        if not row:
            print(f"# {slug}: not found", file=sys.stderr)
            continue
        md = row[0]
        sections = split_into_pillar_sections(md)
        adapter_schema: dict[str, dict] = {}
        for p in PILLARS:
            if p not in sections:
                # Missing pillar — fall back to generic label only
                adapter_schema[p] = {
                    "label": PILLAR_LABELS[p],
                    "subs": [],
                }
                continue
            section = sections[p]
            label = extract_pillar_label(section) or PILLAR_LABELS[p]
            subs = extract_subs_from_section(section)
            if not subs and p == "Q7":
                subs = Q7_FALLBACK
            adapter_schema[p] = {"label": label, "subs": subs}
        out[slug] = adapter_schema

    # BTC monetary_12 — M1-M6 prefix
    btc_row = cur.execute(
        "SELECT markdown_current FROM crypto_adapters WHERE slug = 'btc'"
    ).fetchone()
    if btc_row:
        md = btc_row[0]
        sections = split_into_pillar_sections(md, prefix="M")
        btc_schema: dict[str, dict] = {}
        for p in BTC_PILLARS:
            if p not in sections:
                btc_schema[p] = {"label": BTC_PILLAR_LABELS[p], "subs": []}
                continue
            section = sections[p]
            label = extract_pillar_label_for_prefix(section, "M") or BTC_PILLAR_LABELS[p]
            subs = extract_subs_from_section(section)
            btc_schema[p] = {"label": label, "subs": subs}
        out["btc"] = btc_schema
    else:
        print("# btc: not found", file=sys.stderr)

    # Emit as JS const
    print("// AUTO-GENERATED by tools/extract_adapter_schemas.py — do not edit by hand.")
    print("// Source: /var/lib/ft/ft.db crypto_adapters.markdown_current per adapter slug.")
    print("// Run via: python3 tools/extract_adapter_schemas.py /var/lib/ft/ft.db")
    print()
    print("const D25_SCHEMA_BY_ADAPTER = " + json.dumps(out, indent=2, ensure_ascii=False) + ";")


if __name__ == "__main__":
    main()
