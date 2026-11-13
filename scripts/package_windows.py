#!/usr/bin/env python3
"""Package the Windows build without requiring external native DLLs."""
import json
from pathlib import Path
import sys
import zipfile


def main() -> None:
    source, output = map(Path, sys.argv[1:3])
    version = json.loads(Path("mygo.json").read_text(encoding="utf-8"))["version"]
    output.mkdir(parents=True, exist_ok=True)
    archive = output / f"hua-ci-fan-yi-{version}-windows-amd64.zip"
    instructions = """划词翻译 Windows 试用版

1. 解压整个目录，双击运行划词翻译.exe；应用驻留在任务栏通知区域。
2. 在设置中填写 Base URL、模型和 API Key 并保存。
3. 在其他应用中选中文本，按住鼠标右键 0.5 秒翻译；普通右键短按会在松开后回放。
4. 应用模拟 Ctrl+C 获取选区，剪贴板会被替换。译文可选中后按 Ctrl+C 复制；单击其他位置关闭浮窗。

需要 Windows 10/11 x64。目标应用须支持 Ctrl+C 复制文本。Windows 对向高权限程序模拟输入有系统限制。
本版未经数字签名，Windows 版本尚未完成真机功能及杀毒软件验证。
"""
    with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as package:
        for name in ("划词翻译.exe", "icon.png"):
            package.write(source / name, f"translate-windows-amd64/{name}")
        package.writestr("translate-windows-amd64/使用说明.txt", instructions.encode("utf-8-sig"))
    with zipfile.ZipFile(archive) as package:
        if (bad_file := package.testzip()) is not None:
            raise RuntimeError(f"Failed to verify {bad_file} in {archive}")
    print(archive)


if __name__ == "__main__":
    main()
