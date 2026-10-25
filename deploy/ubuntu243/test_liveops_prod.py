import copy
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest


def load(name):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(name + ".py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


registration = load("register-liveops-prod")
verification = load("verify-liveops-prod")


class RegistrationTests(unittest.TestCase):
    def setUp(self):
        self.payload, self.desired = registration.load_target()
        self.saved = None
        self.writes = []
        self.damage = None
        self.backups = []
        self.beta = {"name": "liveops-beta", "tenant": "default", "is_active": True, "tools": "[]"}

    def api(self, method, path, payload=None, headers=None):
        if method in ("POST", "PUT"):
            self.writes.append(method)
            self.saved = copy.deepcopy(self.desired)
            if self.damage == "readback":
                self.saved["routers"][0]["auth"]["mode"] = "none"
            return {}
        if "versions?" in path:
            if "names=liveops-beta" in path:
                return {"data": [copy.deepcopy(self.beta)]}
            cfg = registration.normalize(self.saved)
            if self.damage == "annotation":
                cfg["tools"][1]["annotations"]["readOnlyHint"] = False
            version = {"name": "liveops-prod", "tenant": "default", "is_active": True}
            for key, field in (("routers", "routers"), ("servers", "servers"), ("tools", "tools"), ("prompts", "prompts"), ("mcpServers", "mcp_servers")):
                version[field] = json.dumps(cfg.get(key))
            return {"data": [version]}
        return {"data": [registration.normalize(self.saved, listed=True)] if self.saved else []}

    def run_registration(self):
        return registration.register(self.payload, self.desired, {}, self.api)

    def test_create_then_reenter(self):
        self.run_registration()
        self.run_registration()
        self.assertEqual(self.writes, ["POST"])

    def test_refuse_drift_and_extra_behavior(self):
        for damage in ("auth", "extra", "annotation", "tools"):
            with self.subTest(damage=damage):
                self.saved = copy.deepcopy(self.desired)
                self.damage = damage
                if damage == "auth":
                    self.saved["routers"][0]["auth"]["mode"] = "none"
                elif damage == "extra":
                    self.saved["tools"][0]["proxy"] = {"host": "unexpected"}
                elif damage == "tools":
                    self.saved["tools"].append(copy.deepcopy(self.saved["tools"][0]))
                with self.assertRaises(RuntimeError):
                    self.run_registration()
                self.assertEqual(self.writes, [])

    def test_readback_failure(self):
        self.damage = "readback"
        with self.assertRaises(RuntimeError):
            self.run_registration()
        self.assertEqual(self.writes, ["POST"])

    def test_prefix_conflict(self):
        self.saved = {"name": "another", "tenant": "default", "routers": self.desired["routers"]}
        with self.assertRaises(RuntimeError):
            self.run_registration()
        self.assertEqual(self.writes, [])

    def baseline(self):
        baseline = copy.deepcopy(self.desired)
        baseline["tools"] = baseline["tools"][:1]
        baseline["servers"][0]["allowedTools"] = [baseline["tools"][0]["name"]]
        self.saved = copy.deepcopy(baseline)
        return baseline

    def backup(self, saved, version):
        self.assertEqual(self.writes, [])
        self.backups.append(copy.deepcopy(version))
        return "fixture-private-backup"

    def test_controlled_update_and_reentry(self):
        baseline = self.baseline()
        registration.register(self.payload, self.desired, {}, self.api, baseline, self.backup)
        registration.register(self.payload, self.desired, {}, self.api, baseline, self.backup)
        self.assertEqual(self.writes, ["PUT"])
        self.assertEqual(len(self.backups), 1)
        self.assertEqual(json.loads(self.backups[0]["tools"]), registration.normalize(baseline)["tools"])

    def test_update_refuses_drift_before_backup(self):
        baseline = self.baseline()
        self.saved["tools"][0]["description"] = "unexpected"
        with self.assertRaises(RuntimeError):
            registration.register(self.payload, self.desired, {}, self.api, baseline, self.backup)
        self.assertEqual(self.writes, [])
        self.assertEqual(self.backups, [])

    def test_backup_failure_prevents_write(self):
        baseline = self.baseline()
        def failed_backup(saved, version):
            raise OSError("fixture")
        with self.assertRaises(OSError):
            registration.register(self.payload, self.desired, {}, self.api, baseline, failed_backup)
        self.assertEqual(self.writes, [])

    def test_update_full_readback_failure(self):
        baseline = self.baseline()
        self.damage = "readback"
        with self.assertRaises(RuntimeError):
            registration.register(self.payload, self.desired, {}, self.api, baseline, self.backup)
        self.assertEqual(self.writes, ["PUT"])
        self.assertEqual(len(self.backups), 1)

    def test_update_checks_full_annotations_and_beta(self):
        baseline = self.baseline()
        def corrupted_api(method, path, payload=None, headers=None):
            result = self.api(method, path, payload, headers)
            if method == "PUT":
                self.damage = "annotation"
            return result
        with self.assertRaises(RuntimeError):
            registration.register(self.payload, self.desired, {}, corrupted_api, baseline, self.backup)
        self.assertEqual(self.writes, ["PUT"])
        self.setUp()
        baseline = self.baseline()
        def changed_beta(method, path, payload=None, headers=None):
            result = self.api(method, path, payload, headers)
            if method == "PUT":
                self.beta["tools"] = "changed"
            return result
        with self.assertRaises(RuntimeError):
            registration.register(self.payload, self.desired, {}, changed_beta, baseline, self.backup)
        self.assertEqual(self.writes, ["PUT"])

    def test_private_full_version_backup(self):
        previous = registration.PRIVATE_ENV
        try:
            with tempfile.TemporaryDirectory() as directory:
                registration.PRIVATE_ENV = Path(directory) / "runtime.env"
                version = {"tools": "fixture-with-annotations", "is_active": True}
                path = registration.backup_version(self.desired, version)
                self.assertEqual(json.loads(path.read_text(encoding="utf-8"))["active_version"], version)
                if os.name != "nt":
                    self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        finally:
            registration.PRIVATE_ENV = previous


class VerificationTests(unittest.TestCase):
    def run_fixture(self, bad_call=False, bad_delete=False):
        calls = []

        def request(url, body, headers, method="POST"):
            calls.append(method)
            if method == "DELETE":
                return (405 if bad_delete else 204), {}, {}, False
            token = headers.get("Authorization")
            if not token:
                return 401, {}, {}, False
            if token.endswith(verification.TOKEN_B) or "liveops-beta" in url:
                return 403, {}, {}, False
            if body["method"] == "initialize":
                return 200, {"result": {}}, {"Mcp-Session-Id": "fixture-session"}, False
            if body["method"] == "notifications/initialized":
                return 202, {}, {}, False
            if body["method"] == "tools/list":
                return 200, {"result": {"tools": [{"name": n, "description": "production"} for n in verification.NAMES]}}, {}, False
            return 200, {"result": {"isError": not bad_call}}, {}, bad_call

        try:
            verification.verify(request)
        finally:
            self.assertEqual(calls[-1], "DELETE")

    def test_acceptance(self):
        self.run_fixture()

    def test_close_on_failure(self):
        with self.assertRaises(RuntimeError):
            self.run_fixture(bad_call=True)

    def test_delete_405_fails(self):
        with self.assertRaises(RuntimeError):
            self.run_fixture(bad_delete=True)


if __name__ == "__main__":
    unittest.main()
