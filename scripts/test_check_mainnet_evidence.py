#!/usr/bin/env python3
"""Unit tests of the NO-GO evidence-integrity preflight, without real audits."""
import hashlib
import importlib.util
import json
import pathlib
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).with_name("check-mainnet-evidence.py")
SPEC = importlib.util.spec_from_file_location("zephyr_mainnet_evidence", SCRIPT)
checker = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(checker)


class EvidenceIntegrityTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = pathlib.Path(self.directory.name)
        self.manifest = self.root / "manifest.json"
        self.gates = {}
        for gate in checker.REQUIRED_GATES:
            path = f"{gate}.log"
            data = f"synthetic fixture only: {gate}\n".encode()
            (self.root / path).write_bytes(data)
            self.gates[gate] = [{"path": path, "sha256": hashlib.sha256(data).hexdigest()}]
        self.write_manifest()

    def write_manifest(self):
        self.manifest.write_text(json.dumps({"schemaVersion": 1, "gates": self.gates}), encoding="utf-8")

    def test_synthetic_complete_manifest_checks_file_integrity_but_not_launch_approval(self):
        self.assertEqual(checker.inspect(self.manifest, self.root), [])

    def test_missing_and_empty_gate_fail_closed(self):
        del self.gates["witness_global_monotonicity_and_freshness"]
        self.gates["wasm_runtime_and_contract_audit"] = []
        self.write_manifest()
        errors = checker.inspect(self.manifest, self.root)
        self.assertTrue(any("missing gate" in e for e in errors), errors)
        self.assertTrue(any("at least one" in e for e in errors), errors)

    def test_mutated_or_missing_artifact_fails(self):
        (self.root / "genesis_and_supply.log").write_text("modified")
        (self.root / "witness_independent_operators.log").unlink()
        errors = checker.inspect(self.manifest, self.root)
        self.assertTrue(any("mismatch" in e for e in errors), errors)
        self.assertTrue(any("witness_independent_operators" in e for e in errors), errors)

    def test_path_traversal_absolute_and_symlink_escape_fail(self):
        outside = tempfile.TemporaryDirectory()
        self.addCleanup(outside.cleanup)
        target = pathlib.Path(outside.name) / "outside.log"
        target.write_bytes(b"outside")
        self.gates["genesis_and_supply"] = [{"path": "../escape.log", "sha256": "0" * 64}]
        self.gates["consensus_byzantine_multihost"] = [{"path": str(target), "sha256": "0" * 64}]
        (self.root / "escape.log").symlink_to(target)
        self.gates["witness_independent_operators"] = [{"path": "escape.log", "sha256": hashlib.sha256(b"outside").hexdigest()}]
        self.write_manifest()
        errors = checker.inspect(self.manifest, self.root)
        self.assertGreaterEqual(len(errors), 3, errors)

    def test_identical_artifact_cannot_fill_distinct_gates(self):
        self.gates["witness_independent_operators"] = list(self.gates["genesis_and_supply"])
        self.write_manifest()
        self.assertTrue(any("reused" in e for e in checker.inspect(self.manifest, self.root)))

    def test_hard_link_alias_cannot_fulfill_separate_gate(self):
        original = self.root / "genesis_and_supply.log"
        alias = self.root / "independent-witness.log"
        alias.hardlink_to(original)
        self.gates["witness_independent_operators"] = [{
            "path": alias.name,
            "sha256": self.gates["genesis_and_supply"][0]["sha256"],
        }]
        self.write_manifest()
        errors = checker.inspect(self.manifest, self.root)
        self.assertTrue(any("hard-linked evidence" in e for e in errors), errors)

    def test_hard_link_outside_dossier_is_rejected(self):
        original = self.root / "genesis_and_supply.log"
        outside = tempfile.TemporaryDirectory()
        self.addCleanup(outside.cleanup)
        (pathlib.Path(outside.name) / "shared-evidence.log").hardlink_to(original)
        errors = checker.inspect(self.manifest, self.root)
        self.assertTrue(any("hard-linked evidence" in e for e in errors), errors)

    def test_invalid_digest_and_unexpected_gate_fail(self):
        self.gates["genesis_and_supply"][0]["sha256"] = "A" * 64
        self.gates["unapproved_waiver"] = []
        self.write_manifest()
        errors = checker.inspect(self.manifest, self.root)
        self.assertTrue(any("SHA-256" in e for e in errors), errors)
        self.assertTrue(any("unknown gate" in e for e in errors), errors)

    def test_symlink_within_root_is_a_duplicate_artifact(self):
        (self.root / "alias.log").symlink_to(self.root / "genesis_and_supply.log")
        self.gates["consensus_byzantine_multihost"] = [{"path": "alias.log", "sha256": self.gates["genesis_and_supply"][0]["sha256"]}]
        self.write_manifest()
        self.assertTrue(any("reused" in e for e in checker.inspect(self.manifest, self.root)))


if __name__ == "__main__":
    unittest.main()
