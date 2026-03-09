"""验证脚本侧分支过滤逻辑，避免未合并分支记忆泄漏到当前搜索结果。"""

from __future__ import annotations

import importlib.util
import os
import subprocess
import tempfile
import unittest
from pathlib import Path


def load_main_module():
    """按文件路径加载脚本模块，避免测试依赖包安装步骤。"""

    module_path = Path(__file__).with_name("main.py")
    spec = importlib.util.spec_from_file_location("memory_manager_main", module_path)
    if spec is None or spec.loader is None:
        raise RuntimeError("无法加载 main.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


MAIN = load_main_module()


class BranchFilterTest(unittest.TestCase):
    """覆盖脚本按分支筛选记忆的核心判断，保证逐条命中行为稳定。"""

    def setUp(self) -> None:
        """构造带已合并和未合并分支的临时仓库，模拟真实项目历史。"""

        self.temp_dir = tempfile.TemporaryDirectory()
        self.repo = Path(self.temp_dir.name)
        env = os.environ | {
            "GIT_AUTHOR_NAME": "OpenCode",
            "GIT_AUTHOR_EMAIL": "opencode@example.com",
            "GIT_COMMITTER_NAME": "OpenCode",
            "GIT_COMMITTER_EMAIL": "opencode@example.com",
        }
        self.run_git("init", "-b", "master", env=env)
        (self.repo / "README.md").write_text("init\n", encoding="utf-8")
        self.run_git("add", "README.md", env=env)
        self.run_git("commit", "-m", "init", env=env)

        self.run_git("checkout", "-b", "feature/merged", env=env)
        (self.repo / "merged.txt").write_text("merged\n", encoding="utf-8")
        self.run_git("add", "merged.txt", env=env)
        self.run_git("commit", "-m", "merged branch", env=env)
        self.run_git("checkout", "master", env=env)
        self.run_git("merge", "--no-ff", "feature/merged", "-m", "merge feature/merged", env=env)

        self.run_git("checkout", "-b", "feature/current", env=env)
        (self.repo / "current.txt").write_text("current\n", encoding="utf-8")
        self.run_git("add", "current.txt", env=env)
        self.run_git("commit", "-m", "current branch", env=env)

        self.run_git("checkout", "master", env=env)
        self.run_git("checkout", "-b", "feature/unmerged", env=env)
        (self.repo / "unmerged.txt").write_text("unmerged\n", encoding="utf-8")
        self.run_git("add", "unmerged.txt", env=env)
        self.run_git("commit", "-m", "unmerged branch", env=env)
        self.run_git("checkout", "feature/current", env=env)

    def tearDown(self) -> None:
        """释放临时仓库，避免测试污染工作区。"""

        self.temp_dir.cleanup()

    def run_git(self, *args: str, env: dict[str, str]) -> None:
        """测试仓库初始化统一走同一入口，减少 setup 里的重复样板。"""

        subprocess.run(["git", *args], cwd=self.repo, env=env, check=True, capture_output=True, text=True)

    def test_filter_hits_by_branch(self) -> None:
        """已合并分支、当前分支和无分支记忆应保留，未合并分支应被过滤。"""

        current_branch = MAIN.resolve_current_git_branch(str(self.repo))
        hits = [
            {"title": "merged", "git_branch": "feature/merged"},
            {"title": "current", "git_branch": "feature/current"},
            {"title": "unmerged", "git_branch": "feature/unmerged"},
            {"title": "shared", "git_branch": ""},
        ]

        filtered = MAIN.filter_hits_by_branch(hits, str(self.repo), current_branch)

        self.assertEqual([hit["title"] for hit in filtered], ["merged", "current", "shared"])

    def test_resolve_default_project_name_prefers_git_remote(self) -> None:
        """存在 Git 远端时应优先使用仓库地址，避免不同本地路径下项目名漂移。"""

        env = os.environ | {
            "GIT_AUTHOR_NAME": "OpenCode",
            "GIT_AUTHOR_EMAIL": "opencode@example.com",
            "GIT_COMMITTER_NAME": "OpenCode",
            "GIT_COMMITTER_EMAIL": "opencode@example.com",
        }
        self.run_git("remote", "add", "origin", "https://github.com/nzlov/hive.git", env=env)

        self.assertEqual(MAIN.resolve_default_project_name(str(self.repo)), "github.com/nzlov/hive.git")

    def test_normalize_git_remote_supports_ssh_style(self) -> None:
        """SSH 风格仓库地址也应转成稳定的 host/path 形式，避免协议差异影响项目隔离。"""

        self.assertEqual(MAIN.normalize_git_remote("git@github.com:nzlov/hive.git"), "github.com/nzlov/hive.git")


class ProjectNameFallbackTest(unittest.TestCase):
    """覆盖非 Git 项目的项目名回退规则，避免客户端在普通目录下生成空项目名。"""

    def test_resolve_default_project_name_falls_back_to_folder_name(self) -> None:
        """没有 Git 远端时应回退到项目文件夹名称，保证搜索与写入仍可隔离。"""

        with tempfile.TemporaryDirectory() as temp_dir:
            project_root = Path(temp_dir) / "demo-project"
            project_root.mkdir()
            self.assertEqual(MAIN.resolve_default_project_name(str(project_root)), "demo-project")


class APITokenConfigTest(unittest.TestCase):
    """覆盖 API Token 配置解析，避免项目级和默认级配置优先级回归。"""

    def test_resolve_api_token_supports_auth_section(self) -> None:
        """支持从 auth 段读取 token，避免配置结构升级后旧脚本无法请求服务端。"""

        self.assertEqual(MAIN.resolve_api_token({"auth": {"api_token": "demo-token"}}), "demo-token")

    def test_resolve_request_target_prefers_project_token(self) -> None:
        """项目级 token 应覆盖默认 token，避免多项目共用服务时误用错误身份。"""

        with tempfile.TemporaryDirectory() as temp_dir:
            project_root = Path(temp_dir) / "demo-project"
            project_root.mkdir()
            config = {
                "default_server_base_url": "http://127.0.0.1:8080",
                "api_token": "default-token",
                "projects": {
                    str(project_root): {
                        "alias": "demo-alias",
                        "api_token": "project-token",
                    }
                },
            }
            resolved_root, base_url, project_name, api_token = MAIN.resolve_request_target(config, str(project_root))

            self.assertEqual(resolved_root, str(project_root.resolve()))
            self.assertEqual(base_url, "http://127.0.0.1:8080")
            self.assertEqual(project_name, "demo-alias")
            self.assertEqual(api_token, "project-token")


class WriteItemsFileTest(unittest.TestCase):
    """覆盖 --items-file 输入和写入后清理，保证 Windows 场景下批量写入稳定。"""

    def test_parse_write_items_reads_items_file(self) -> None:
        """通过文件传入 JSON 时应正确解析记忆数组，避免命令行转义干扰。"""

        with tempfile.TemporaryDirectory() as temp_dir:
            items_path = Path(temp_dir) / "items.json"
            items_path.write_text(
                '[{"type":"summary","title":"标题","tags":["标签"],"summary":"简介","context":"正文"}]',
                encoding="utf-8",
            )
            args = type("Args", (), {"items_json": "", "items_file": str(items_path)})()

            items = MAIN.parse_write_items(args)

            self.assertEqual(len(items), 1)
            self.assertEqual(items[0]["type"], "summary")
            self.assertEqual(items[0]["title"], "标题")

    def test_parse_write_items_requires_exactly_one_source(self) -> None:
        """--items-json 与 --items-file 同时提供或同时缺失都应报错，避免来源歧义。"""

        args_both = type("Args", (), {"items_json": "[]", "items_file": "items.json"})()
        with self.assertRaises(SystemExit):
            MAIN.parse_write_items(args_both)

        args_none = type("Args", (), {"items_json": "", "items_file": ""})()
        with self.assertRaises(SystemExit):
            MAIN.parse_write_items(args_none)

    def test_run_write_deletes_items_file_after_success(self) -> None:
        """写入成功后应自动删除记忆文件，避免明文内容残留到本地磁盘。"""

        with tempfile.TemporaryDirectory() as temp_dir:
            items_path = Path(temp_dir) / "items.json"
            items_path.write_text(
                '[{"type":"error","title":"标题","tags":[],"summary":"","context":"正文"}]',
                encoding="utf-8",
            )

            original_post_json = MAIN.post_json
            original_resolve_current_git_branch = MAIN.resolve_current_git_branch
            try:
                MAIN.post_json = lambda *args, **kwargs: {}
                MAIN.resolve_current_git_branch = lambda *_args, **_kwargs: ""
                args = type(
                    "Args",
                    (),
                    {"items_json": "", "items_file": str(items_path)},
                )()

                self.assertEqual(
                    MAIN.run_write(args, ".", "http://127.0.0.1:8080", "demo", "token"),
                    0,
                )
            finally:
                MAIN.post_json = original_post_json
                MAIN.resolve_current_git_branch = original_resolve_current_git_branch

            self.assertFalse(items_path.exists())


class SearchRenderTest(unittest.TestCase):
    """覆盖搜索结果渲染，避免脚本继续依赖已移除的旧返回字段。"""

    def test_render_hit_includes_structured_metadata(self) -> None:
        """脚本重渲染搜索结果时应只展示用户可读元信息，避免泄露筛选辅助字段。"""

        lines = MAIN.render_hit(
            {
                "source": "summary",
                "git_branch": "feature/demo",
                "title": "连接池复用策略",
                "tags": ["数据库", "连接池"],
                "timestamp": "2026-03-09T12:00:00Z",
                "confidence": 0.9,
                "snippets": [
                    {"start": 3, "end": 6, "content": "## Fix\n\n- 方案: 统一复用长连接池。"}
                ],
            },
            1,
        )

        rendered = "\n".join(lines)
        self.assertIn("- title: 连接池复用策略", rendered)
        self.assertIn("- tags: 数据库, 连接池", rendered)
        self.assertNotIn("- source:", rendered)
        self.assertNotIn("- git_branch:", rendered)

    def test_run_search_keeps_git_branch_for_filtering(self) -> None:
        """按分支筛选后仍应保留 git_branch 字段，避免过滤阶段失去必要依据。"""

        response = {
            "error_hits": [],
            "summary_hits": [
                {
                    "source": "summary",
                    "git_branch": "feature/demo",
                    "title": "标题",
                    "tags": ["标签"],
                    "timestamp": "2026-03-09T12:00:00Z",
                    "confidence": 0.8,
                    "file_content": "正文",
                }
            ],
        }

        original_post_json = MAIN.post_json
        original_resolve_current_git_branch = MAIN.resolve_current_git_branch
        original_filter_hits_by_branch = MAIN.filter_hits_by_branch
        original_render_search_markdown = MAIN.render_search_markdown
        captured: dict[str, object] = {}
        try:
            MAIN.post_json = lambda *args, **kwargs: response
            MAIN.resolve_current_git_branch = lambda *_args, **_kwargs: "feature/demo"
            MAIN.filter_hits_by_branch = lambda hits, *_args, **_kwargs: hits

            def fake_render_search_markdown(_response, _error_hits, summary_hits):
                captured["summary_hits"] = summary_hits
                return "ok"

            MAIN.render_search_markdown = fake_render_search_markdown
            args = type("Args", (), {"query": ["标题"], "debug": False})()
            self.assertEqual(
                MAIN.run_search(args, ".", "http://127.0.0.1:8080", "demo", "token"),
                0,
            )
        finally:
            MAIN.post_json = original_post_json
            MAIN.resolve_current_git_branch = original_resolve_current_git_branch
            MAIN.filter_hits_by_branch = original_filter_hits_by_branch
            MAIN.render_search_markdown = original_render_search_markdown

        summary_hits = captured.get("summary_hits")
        self.assertIsInstance(summary_hits, list)
        self.assertEqual(summary_hits[0]["git_branch"], "feature/demo")


class ConfigBootstrapTest(unittest.TestCase):
    """覆盖缺省配置文件初始化，避免首次运行时因目录或文件缺失直接失败。"""

    def test_load_config_creates_missing_config_and_parent_dir(self) -> None:
        """配置文件不存在时应自动创建父目录和默认配置骨架，保证后续读取稳定。"""

        with tempfile.TemporaryDirectory() as temp_dir:
            original_path = MAIN.CONFIG_PATH
            config_path = Path(temp_dir) / ".config" / "hive" / "config.json"
            MAIN.CONFIG_PATH = config_path
            try:
                payload = MAIN.load_config()
            finally:
                MAIN.CONFIG_PATH = original_path

            self.assertEqual(payload, MAIN.default_config_payload())
            self.assertTrue(config_path.parent.is_dir())
            self.assertTrue(config_path.is_file())
            self.assertEqual(
                config_path.read_text(encoding="utf-8"),
                '{\n  "default_server_base_url": "http://127.0.0.1:8080",\n  "api_token": "",\n  "projects": {}\n}\n',
            )


if __name__ == "__main__":
    unittest.main()
