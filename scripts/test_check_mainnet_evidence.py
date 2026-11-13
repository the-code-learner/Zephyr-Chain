#!/usr/bin/env python3
"""Unit tests of the NO-GO evidence-integrity preflight, without real audits."""
import hashlib
import importlib.util
import json
import pathlib
import os
import tempfile
import unittest
from unittest import mock

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

    def test_duplicate_json_top_level_key_fails_closed(self):
        raw = self.manifest.read_text(encoding="utf-8")
        self.manifest.write_text(
            raw.replace('"schemaVersion": 1,', '"schemaVersion": 1, "schemaVersion": 1,', 1),
            encoding="utf-8",
        )
        errors = checker.inspect(self.manifest, self.root)
        self.assertTrue(any("duplicate JSON key: schemaVersion" in e for e in errors), errors)

    def test_duplicate_json_gate_key_fails_closed(self):
        raw = self.manifest.read_text(encoding="utf-8")
        self.manifest.write_text(
            raw.replace('"genesis_and_supply":', '"genesis_and_supply": [], "genesis_and_supply":', 1),
            encoding="utf-8",
        )
        errors = checker.inspect(self.manifest, self.root)
        self.assertTrue(any("duplicate JSON key: genesis_and_supply" in e for e in errors), errors)

    def test_duplicate_json_artifact_key_fails_closed(self):
        raw = self.manifest.read_text(encoding="utf-8")
        self.manifest.write_text(
            raw.replace('"path": "genesis_and_supply.log"',
                        '"path": "shadow.log", "path": "genesis_and_supply.log"', 1),
            encoding="utf-8",
        )
        errors = checker.inspect(self.manifest, self.root)
        self.assertTrue(any("duplicate JSON key: path" in e for e in errors), errors)

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

    def test_symlink_to_existing_in_root_file_is_rejected(self):
        (self.root / "alias.log").symlink_to(self.root / "genesis_and_supply.log")
        self.gates["consensus_byzantine_multihost"] = [{
            "path": "alias.log",
            "sha256": self.gates["genesis_and_supply"][0]["sha256"],
        }]
        self.write_manifest()
        errors = checker.inspect(self.manifest, self.root)
        self.assertTrue(any("symlink evidence path component" in e for e in errors), errors)

    def test_symlinked_intermediate_directory_is_rejected(self):
        (self.root / "actual").mkdir()
        data = b"independent synthetic fixture"
        (self.root / "actual" / "witness.txt").write_bytes(data)
        (self.root / "alias-dir").symlink_to(self.root / "actual", target_is_directory=True)
        self.gates["witness_independent_operators"] = [{
            "path": "alias-dir/witness.txt",
            "sha256": hashlib.sha256(data).hexdigest(),
        }]
        self.write_manifest()
        errors = checker.inspect(self.manifest, self.root)
        self.assertTrue(any("symlink evidence path component" in e for e in errors), errors)

    def test_real_nested_regular_file_is_accepted(self):
        (self.root / "reports").mkdir()
        data = b"nested synthetic evidence"
        (self.root / "reports" / "report.txt").write_bytes(data)
        self.gates["witness_independent_operators"] = [{
            "path": "reports/report.txt",
            "sha256": hashlib.sha256(data).hexdigest(),
        }]
        self.write_manifest()
        self.assertEqual(checker.inspect(self.manifest, self.root), [])



    def test_symlink_swap_between_lstat_and_open_is_rejected(self):
        """The final read must never follow a leaf replaced after lstat."""
        outside = tempfile.TemporaryDirectory()
        self.addCleanup(outside.cleanup)
        remote = pathlib.Path(outside.name) / "remote.log"
        remote.write_bytes(b"attacker supplied matching evidence")
        target = self.root / "genesis_and_supply.log"
        self.gates["genesis_and_supply"][0]["sha256"] = hashlib.sha256(remote.read_bytes()).hexdigest()
        self.write_manifest()
        original_open = os.open
        swapped = False

        def swap_before_descriptor_open(path, flags, mode=0o777, *, dir_fd=None):
            nonlocal swapped
            if path == target.name and dir_fd is not None and not swapped:
                swapped = True
                target.unlink()
                target.symlink_to(remote)
            return original_open(path, flags, mode, dir_fd=dir_fd)

        with mock.patch.object(checker.os, "open", side_effect=swap_before_descriptor_open):
            errors = checker.inspect(self.manifest, self.root)
        self.assertTrue(swapped, "test never exercised the lstat/open swap")
        self.assertTrue(any("genesis_and_supply" in issue for issue in errors), errors)
        self.assertEqual(remote.read_bytes(), b"attacker supplied matching evidence")

    def test_intermediate_directory_swap_before_open_is_rejected(self):
        """A parent swapped after lstat cannot redirect a rooted openat."""
        actual = self.root / "evidence-subdir"
        actual.mkdir()
        data = b"nested source"
        (actual / "evidence.log").write_bytes(data)
        self.gates["witness_independent_operators"] = [{
            "path": "evidence-subdir/evidence.log",
            "sha256": hashlib.sha256(data).hexdigest(),
        }]
        self.write_manifest()
        outside = tempfile.TemporaryDirectory()
        self.addCleanup(outside.cleanup)
        (pathlib.Path(outside.name) / "evidence.log").write_bytes(data)
        original_open = os.open
        swapped = False

        def swap_parent(path, flags, mode=0o777, *, dir_fd=None):
            nonlocal swapped
            if path == "evidence-subdir" and dir_fd is not None and not swapped:
                swapped = True
                actual.rename(self.root / "original-subdir")
                actual.symlink_to(outside.name, target_is_directory=True)
            return original_open(path, flags, mode, dir_fd=dir_fd)

        with mock.patch.object(checker.os, "open", side_effect=swap_parent):
            errors = checker.inspect(self.manifest, self.root)
        self.assertTrue(swapped, "test never exercised the directory swap")
        self.assertTrue(any("witness_independent_operators" in issue for issue in errors), errors)


    def test_manifest_symlink_and_hardlink_are_rejected(self):
        original = self.root / "original-manifest.json"
        self.manifest.rename(original)
        self.manifest.symlink_to(original)
        errors = checker.inspect(self.manifest, self.root)
        self.assertTrue(any("manifest unreadable" in e for e in errors), errors)

        self.manifest.unlink()
        self.manifest.hardlink_to(original)
        errors = checker.inspect(self.manifest, self.root)
        self.assertTrue(any("hard-linked manifest" in e for e in errors), errors)

    def test_oversized_manifest_is_rejected_before_json_decode(self):
        self.manifest.write_bytes(b" " * (checker.MAX_MANIFEST_BYTES + 1))
        errors = checker.inspect(self.manifest, self.root)
        self.assertTrue(any("manifest exceeds byte limit" in e for e in errors), errors)

    @unittest.skipUnless(hasattr(os, "mkfifo"), "POSIX FIFO required")
    def test_manifest_fifo_fails_closed_without_blocking(self):
        self.manifest.unlink()
        os.mkfifo(self.manifest)
        errors = checker.inspect(self.manifest, self.root)
        self.assertTrue(any("manifest is not a regular file" in e for e in errors), errors)

    def test_manifest_symlink_swap_before_descriptor_open_fails_closed(self):
        alternate = self.root / "alternate-manifest.json"
        alternate.write_bytes(self.manifest.read_bytes())
        original_open = os.open
        swapped = False

        def swap_manifest(path, flags, mode=0o777, *, dir_fd=None):
            nonlocal swapped
            if path == self.manifest and not swapped:
                swapped = True
                self.manifest.unlink()
                self.manifest.symlink_to(alternate)
            return original_open(path, flags, mode, dir_fd=dir_fd)

        with mock.patch.object(checker.os, "open", side_effect=swap_manifest):
            errors = checker.inspect(self.manifest, self.root)
        self.assertTrue(swapped, "manifest swap was not exercised")
        self.assertTrue(any("manifest unreadable" in e for e in errors), errors)

    def test_manifest_mutation_during_read_is_rejected(self):
        original_read = os.read
        mutated = False

        def mutate_during_read(fd, count):
            nonlocal mutated
            data = original_read(fd, count)
            if not mutated:
                mutated = True
                self.manifest.write_bytes(self.manifest.read_bytes() + b" ")
            return data

        with mock.patch.object(checker.os, "read", side_effect=mutate_during_read):
            errors = checker.inspect(self.manifest, self.root)
        self.assertTrue(mutated, "manifest race was not exercised")
        self.assertTrue(any("manifest modified during validation" in e for e in errors), errors)


if __name__ == "__main__":
    unittest.main()
