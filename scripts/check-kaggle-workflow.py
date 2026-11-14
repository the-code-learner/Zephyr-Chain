#!/usr/bin/env python3
"""Fail-closed static checks for the CPU-only Kaggle Go test workflow.

This check runs in regular GitHub CI before the workflow can reach main.
It requires no Kaggle API token or notebook compute.
"""
import ast
from pathlib import Path
import re


ROOT = Path(__file__).resolve().parent.parent
script = (ROOT / "scripts/kaggle_go_consensus_cpu.py").read_text()
workflow = (ROOT / ".github/workflows/kaggle-real-go-cpu.yml").read_text()

tree = ast.parse(script)
expected = None
for node in tree.body:
    if isinstance(node, ast.Assign):
        if any(isinstance(target, ast.Name) and target.id == "EXPECTED_TESTS"
               for target in node.targets):
            expected = ast.literal_eval(node.value)

assert isinstance(expected, tuple) and len(expected) >= 15
assert len(set(expected)) == len(expected)
assert all(re.fullmatch(r"Test[A-Za-z0-9_]+", name) for name in expected)

# The remote acceptance gate must independently list the same exact names.
block = re.search(r"^\s+required_tests = \{\n(?P<body>(?:\s+\"Test[^\n]+\n)+)\s+\}$",
                  workflow, re.MULTILINE)
assert block, "missing exact remote required_tests block"
in_workflow = re.findall(r'^\s+"(Test[A-Za-z0-9_]+)",$', block["body"], re.MULTILINE)
assert len(in_workflow) == len(expected) and set(in_workflow) == set(expected)

# Each added preflight selection must be one complete quoted shell argument.
preflight = re.findall(r"^\s+-run '(\^\(Test[^\n]*\)\$)'$", workflow, re.MULTILINE)
assert len(preflight) == 2, "expected two complete anchored Go preflight filters"
assert all(pattern.startswith("^(") and pattern.endswith(")$") for pattern in preflight)
for pattern in preflight:
    assert all(name in expected for name in pattern[2:-2].split("|"))

# Duplicate workflow tails or a GPU/TPU opt-in must never pass CI.
assert workflow.count("retention-days: 14") == 1
assert workflow.count("name: Kaggle Offline Real Go Consensus CPU") == 1
assert workflow.count("- name: Preserve only nonsecret test evidence") == 1
for line in (
    '"enable_gpu": "false"',
    '"enable_tpu": "false"',
    '"enable_internet": "false"',
    '"machine_shape": ""',
    "or report.get(\"passed_test_names\") != sorted(required_tests)",
    "or report.get(\"skipped_test_cases\") != 0",
):
    assert line in workflow, f"missing safety invariant: {line}"
assert "kaggle kernels push" in workflow
print("Kaggle CPU-only workflow static invariants OK:", len(expected), "named tests")
