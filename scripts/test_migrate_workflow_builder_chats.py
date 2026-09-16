import importlib.util
import json
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("migrate_workflow_builder_chats.py")
SPEC = importlib.util.spec_from_file_location("migration", SCRIPT)
migration = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(migration)


class WorkflowBuilderChatMigrationTest(unittest.TestCase):
    def test_dry_run_apply_and_idempotency(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            conversation = root / "Workflow" / "demo" / "builder" / "conversation"
            legacy = conversation / "2026-09-15" / "session-owned-conversation.json"
            legacy.parent.mkdir(parents=True)
            legacy.write_text(json.dumps({"session_id": "owned", "conversation_history": []}), encoding="utf-8")
            source_rel = legacy.relative_to(root).as_posix()
            index = {
                "entries": {
                    source_rel: {
                        "session": {
                            "session_id": "owned",
                            "user_id": "default",
                            "username": "System / legacy",
                            "conversation_path": source_rel,
                        }
                    }
                }
            }
            (conversation / "chat-index.json").write_text(json.dumps(index), encoding="utf-8")
            owners = {"owned": {"user_id": "user-1", "username": "Alice"}}

            planned, changed = migration.migrate(root, owners, False)
            self.assertEqual((planned, changed), (1, 0))
            self.assertTrue(legacy.exists())

            planned, changed = migration.migrate(root, owners, True)
            self.assertEqual((planned, changed), (1, 1))
            destination = conversation / "users" / "user-1" / "2026-09-15" / legacy.name
            self.assertTrue(destination.exists())
            self.assertFalse(legacy.exists())
            record = json.loads(destination.read_text(encoding="utf-8"))
            self.assertEqual(record["user_id"], "user-1")
            updated_index = json.loads((conversation / "chat-index.json").read_text(encoding="utf-8"))
            destination_rel = destination.relative_to(root).as_posix()
            self.assertIn(destination_rel, updated_index["entries"])
            self.assertEqual(updated_index["entries"][destination_rel]["session"]["workspace_path"], "Workflow/demo")

            self.assertEqual(migration.migrate(root, owners, True), (0, 0))

    def test_unowned_chat_moves_to_system(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            legacy = root / "Workflow" / "demo" / "builder" / "conversation" / "2026-09-15" / "session-system-conversation.json"
            legacy.parent.mkdir(parents=True)
            legacy.write_text(json.dumps({"session_id": "system"}), encoding="utf-8")
            migration.migrate(root, {}, True)
            destination = legacy.parents[1] / "system" / "2026-09-15" / legacy.name
            record = json.loads(destination.read_text(encoding="utf-8"))
            self.assertEqual(record["username"], "System / legacy")

    def test_work_and_crew_project_chats_are_out_of_scope(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / "Workflow").mkdir()
            crew_chat = root / "_users" / "alice" / "Chats" / "Work" / "projects" / "demo" / "builder" / "conversation" / "2026-09-15" / "session-crew-conversation.json"
            crew_chat.parent.mkdir(parents=True)
            original = {"session_id": "crew", "user_id": "alice"}
            crew_chat.write_text(json.dumps(original), encoding="utf-8")

            self.assertEqual(migration.migrate(root, {}, True), (0, 0))
            self.assertEqual(json.loads(crew_chat.read_text(encoding="utf-8")), original)


if __name__ == "__main__":
    unittest.main()
