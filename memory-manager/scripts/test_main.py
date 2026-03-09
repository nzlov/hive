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
            {"path": "1", "git_branch": "feature/merged"},
            {"path": "2", "git_branch": "feature/current"},
            {"path": "3", "git_branch": "feature/unmerged"},
            {"path": "4", "git_branch": ""},
        ]

        filtered = MAIN.filter_hits_by_branch(hits, str(self.repo), current_branch)

        self.assertEqual([hit["path"] for hit in filtered], ["1", "2", "4"])


if __name__ == "__main__":
    unittest.main()
