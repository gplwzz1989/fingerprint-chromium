"""复用现有 Ninja 文件进行单文件语法编译，不调用 GN 或生成依赖。"""
import argparse
import ctypes
from pathlib import Path
import re
import subprocess
import sys


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", help="Chromium 源码根目录下的相对路径")
    parser.add_argument("--out", default="build/src/out/Development")
    parser.add_argument("--define", action="append", default=[])
    parser.add_argument("--compile", action="store_true", help="只编译指定文件的目标对象，不链接")
    parser.add_argument("--input-override", help="使用相同参数验证独立测试源文件")
    parser.add_argument("--object-output", help="为独立测试指定对象文件输出路径")
    args = parser.parse_args()
    if sys.platform != "win32":
        parser.error("该工具仅用于 Windows 上已生成的 Chromium 构建目录")
    out = Path(args.out).resolve()
    source = args.source.replace("\\", "/")
    root = out.parent.parent
    if not (root / source).is_file() or not (root / source).resolve().is_relative_to(root):
        parser.error("源码路径无效")
    needle = ": cxx ../../" + source + " "
    module = None
    output = None
    for file in (out / "obj").rglob("*.ninja"):
        text = file.read_text(encoding="utf-8")
        if needle in text:
            module = text
            edge = next(line for line in text.splitlines() if needle in line)
            output = edge.split(": cxx ", 1)[0].removeprefix("build ")
            break
    if module is None:
        parser.error("现有构建目录没有该文件的编译规则")
    variables = dict(re.findall(r"^([a-z_]+) = (.*)$", module, re.MULTILINE))
    variables.update(in_= "../../" + source)
    variables["in"] = "../../" + source
    variables["out"] = output if args.compile else "syntax-check.obj"
    if args.input_override:
        test_input = Path(args.input_override).resolve()
        if not test_input.is_file():
            parser.error("独立测试源文件不存在")
        if args.compile and not args.object_output:
            parser.error("编译独立测试必须指定对象文件，避免覆盖原编译输出")
        variables["in"] = '"' + test_input.as_posix() + '"'
    if args.object_output:
        variables["out"] = '"' + Path(args.object_output).resolve().as_posix() + '"'
    variables.setdefault("target_out_dir", "obj/syntax-check")
    variables.setdefault("label_name", "syntax-check")
    variables.setdefault("module_deps_no_self", "")
    toolchain = (out / "toolchain.ninja").read_text(encoding="utf-8")
    match = re.search(r"^rule cxx\n  command = (.*)$", toolchain, re.MULTILINE)
    if not match:
        parser.error("现有工具链没有 C++ 编译规则")
    command = match.group(1)
    for _ in range(10):
        expanded = re.sub(r"\$\{([a-z_]+)\}", lambda m: variables.get(m[1], m[0]), command)
        if expanded == command:
            break
        command = expanded
    if re.search(r"\$\{[a-z_]+\}", command):
        parser.error("编译参数含有尚未解析的变量，未执行编译")
    command = command.replace("$:", ":").replace("$ ", " ").replace("$$", "$")
    command = re.sub(r"\s+/showIncludes(?::\w+)?", "", command)
    if not args.compile:
        command = re.sub(r"\s+/c(?=\s)", "", command)
    command += " /Y-"
    if not args.compile:
        command += " /clang:-fsyntax-only"
    argc = ctypes.c_int()
    split = ctypes.windll.shell32.CommandLineToArgvW
    split.argtypes = [ctypes.c_wchar_p, ctypes.POINTER(ctypes.c_int)]
    split.restype = ctypes.POINTER(ctypes.c_wchar_p)
    argv = split(command, ctypes.byref(argc))
    if not argv:
        parser.error("无法解析本地编译命令")
    try:
        command_args = [argv[i] for i in range(argc.value)]
    finally:
        ctypes.windll.kernel32.LocalFree(ctypes.cast(argv, ctypes.c_void_p))
    command_args.extend("-D" + value for value in args.define)
    print(("单文件目标编译：" if args.compile else "局部语法编译：") + (args.input_override or source), flush=True)
    return subprocess.run(command_args, cwd=out).returncode


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8")
    sys.exit(main())
