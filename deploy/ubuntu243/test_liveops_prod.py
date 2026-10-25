import copy
import importlib.util
import json
from pathlib import Path
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

    def api(self, method, path, payload=None, headers=None):
        if method == "POST":
            self.writes.append(method)
            self.saved = copy.deepcopy(self.desired)
            if self.damage == "readback":
                self.saved["routers"][0]["auth"]["mode"] = "none"
            return {}
        if "versions?" in path:
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
