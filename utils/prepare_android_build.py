#!/usr/bin/env python3
"""在 WSL 中检查 Android 构建配置；仅显式更新已确认的参数，不运行构建。"""

import argparse
import json
import pathlib
import re
import shlex
import subprocess
import sys


class ConfigurationError(Exception):
    pass


class ChineseArgumentParser(argparse.ArgumentParser):
    def format_help(self):
        return super().format_help().replace("usage: ", "用法：", 1)

    def error(self, message):
        self.exit(2, "参数不正确，请使用 --help 查看用法。\n")


def require_path(value, label, directory=False):
    path = pathlib.Path(value)
    if not path.is_absolute() or re.match(r"^[A-Za-z]:", value):
        raise ConfigurationError(f"{label}必须是 WSL 中的绝对路径。")
    exists = path.is_dir() if directory else path.is_file()
    if not exists:
        raise ConfigurationError(f"{label}不存在：{path}")
    return path


def check_tool(path, label, argument="--version", elf=True):
    if elf:
        with path.open("rb") as stream:
            if stream.read(4) != b"\x7fELF":
                raise ConfigurationError(f"{label}不是 Linux 可执行文件：{path}")
    try:
        result = subprocess.run(
            [str(path), argument], capture_output=True, text=True,
            encoding="utf-8", errors="replace", timeout=15, check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        raise ConfigurationError(f"{label}无法执行，请检查 WSL 工具链。") from None
    if result.returncode:
        raise ConfigurationError(f"{label}版本检查失败，请检查 WSL 工具链。")
    output = (result.stdout + result.stderr).strip()
    if not output:
        raise ConfigurationError(f"{label}未返回版本信息。")
    print(f"{label}：{output.splitlines()[0]}")
    return output


def gn_value(content, key):
    match = re.search(
        rf'^\s*{re.escape(key)}\s*=\s*("[^"\n]*"|[^\s#]+)',
        content, re.MULTILINE,
    )
    if not match:
        raise ConfigurationError(f"Android 模板缺少参数：{key}")
    return match.group(1).strip('"')


def prepare(apply):
    if sys.platform != "linux":
        raise ConfigurationError("Android 配置检查必须在 WSL Linux 内执行。")
    if "microsoft" not in pathlib.Path("/proc/sys/kernel/osrelease").read_text().lower():
        raise ConfigurationError("此入口仅用于项目指定的 WSL Linux 环境。")

    repository = pathlib.Path(__file__).resolve().parent.parent
    roots = json.loads((repository / "build-configs/build-roots.json").read_text(
        encoding="utf-8-sig"))
    config = roots["android"]["arm64"]
    for key in ("source_dir", "output_dir", "gn_path", "ninja_path", "clang_dir",
                "rust_dir", "bindgen_dir", "java_home", "sdk_dir", "ndk_dir"):
        if not isinstance(config.get(key), str) or not config[key]:
            raise ConfigurationError(f"Android 路径清单缺少登记：{key}。请先确认并补齐配置。")
    source = require_path(config["source_dir"], "Chromium 源码目录", True)
    if source.resolve() != (repository / "build/src").resolve():
        raise ConfigurationError("登记的 Chromium 源码目录与当前仓库不一致。")
    output = require_path(config["output_dir"], "Android 活跃输出目录", True)
    if output.resolve() in {source / "out/Release", source / "out/Development"}:
        raise ConfigurationError("Android 输出不能使用 PC 构建目录。")
    args_file = require_path(str(output / "args.gn"), "Android 实际参数文件")
    require_path(str(output / "build.ninja"), "Android 构建图")
    current = args_file.read_text(encoding="utf-8-sig")
    if gn_value(current, "target_os") != "android" or gn_value(current, "target_cpu") != "arm64":
        raise ConfigurationError("登记的输出不是 Android ARM64，禁止覆盖配置。")

    template = (repository / "build-configs/android-arm64.gn").read_bytes()
    content = template.decode("utf-8-sig")
    if re.search(r'^\s*import\s*\(', content, re.MULTILINE):
        raise ConfigurationError("Android 模板必须独立，不能导入 PC 配置。")
    if gn_value(content, "target_os") != "android" or gn_value(content, "target_cpu") != "arm64":
        raise ConfigurationError("Android 模板的平台或 ABI 不正确。")

    tool_paths = {
        "gn": require_path(config["gn_path"], "Linux GN"),
        "ninja": require_path(config["ninja_path"], "Linux Ninja"),
        "clang": require_path(str(pathlib.Path(config["clang_dir"]) / "bin/clang"), "Linux Clang"),
        "rustc": require_path(str(pathlib.Path(config["rust_dir"]) / "bin/rustc"), "Linux Rust"),
        "bindgen": require_path(str(pathlib.Path(config["bindgen_dir"]) / "bin/bindgen"), "Linux bindgen"),
        "javac": require_path(str(pathlib.Path(config["java_home"]) / "bin/javac"), "Linux JDK"),
    }
    if pathlib.Path(config["java_home"]).resolve() != (source / "third_party/jdk/current").resolve():
        raise ConfigurationError("JDK 登记必须与 Chromium 实际使用的源码内 JDK 一致。")
    for name, path in tool_paths.items():
        version = check_tool(path, name, "-version" if name == "javac" else "--version")
        if name == "rustc" and version.splitlines()[0] != gn_value(content, "rustc_version"):
            raise ConfigurationError("Rust 实际版本与模板记录不一致。")
    for name in ("clang++", "ld.lld", "llvm-ar"):
        path = require_path(str(pathlib.Path(config["clang_dir"]) / "bin" / name), f"Linux {name}")
        check_tool(path, name)
    java = require_path(str(pathlib.Path(config["java_home"]) / "bin/java"), "Chromium Java 运行时")
    check_tool(java, "Java 运行时", "-version", elf=False)

    with (output / "build.ninja").open(encoding="utf-8") as stream:
        header = [stream.readline() for _ in range(20)]
    generator = next((line.strip().removeprefix("command = ")
                      for line in header if line.strip().startswith("command = ")), "")
    command = shlex.split(generator)
    if not command or (output / command[0]).resolve() != tool_paths["gn"].resolve():
        raise ConfigurationError("Android 构建图未使用登记的独立 Linux GN，请核对构建图。")
    root_argument = next((word[7:] for word in command if word.startswith("--root=")), "")
    if not root_argument or (output / root_argument).resolve() != source.resolve():
        raise ConfigurationError("Android 构建图的源码根目录与路径清单不一致。")

    for key, config_key in (
        ("clang_base_path", "clang_dir"),
        ("rust_sysroot_absolute", "rust_dir"),
        ("rust_bindgen_root", "bindgen_dir"),
        ("android_sdk_root", "sdk_dir"),
        ("android_ndk_root", "ndk_dir"),
    ):
        if pathlib.Path(gn_value(content, key)).resolve() != pathlib.Path(config[config_key]).resolve():
            raise ConfigurationError(f"模板与路径清单不一致：{key}")

    sdk = require_path(config["sdk_dir"], "Android SDK", True)
    ndk = require_path(config["ndk_dir"], "Android NDK", True)
    properties = (ndk / "source.properties").read_text(encoding="utf-8")
    if "Pkg.Revision = 28.0.13004108" not in properties or gn_value(content, "android_ndk_version") != "r28":
        raise ConfigurationError("Android NDK 版本与已确认的 r28 配置不一致。")
    require_path(str(sdk / "platforms" / ("android-" + gn_value(content, "android_sdk_platform_version")) / "android.jar"), "Android 平台 SDK")
    require_path(str(sdk / "build-tools" / gn_value(content, "android_sdk_build_tools_version") / "aapt2"), "Android 构建工具")
    aapt2 = require_path(str(pathlib.Path(gn_value(content, "android_sdk_tools_bundle_aapt2_dir")) / "aapt2"), "实际 AAPT2")
    check_tool(aapt2, "AAPT2", "version")
    stdlib = require_path(str(pathlib.Path(config["rust_dir"]) / "lib/rustlib/aarch64-linux-android/lib"), "Rust Android ARM64 标准库", True)
    if not any(stdlib.glob("libstd-*.rlib")):
        raise ConfigurationError("Rust 工具链缺少 Android ARM64 标准库。")

    if args_file.read_bytes() == template:
        print(f"Android 配置与模板一致，未写入文件：{output}")
    elif not apply:
        raise ConfigurationError("Android 实际参数与模板不同。请核对差异并确认后使用 --apply 更新；本次未写入文件。")
    else:
        with args_file.open("wb") as stream:
            stream.write(template)
        print(f"已原地更新已确认的 Android 配置：{args_file}")
    print("WSL 工具与配置检查通过；尚未重生成构建图或执行编译。")


def main():
    parser = ChineseArgumentParser(description=__doc__, add_help=False)
    parser._optionals.title = "选项"
    parser.add_argument("-h", "--help", action="help", help="显示使用说明")
    parser.add_argument("--apply", action="store_true", help="写入已确认的模板；不执行 GN 或编译")
    arguments = parser.parse_args()
    try:
        prepare(arguments.apply)
    except ConfigurationError as error:
        print(f"检查未通过：{error}", file=sys.stderr)
        return 1
    except (OSError, ValueError, KeyError, TypeError):
        print("检查未通过：构建路径登记或配置文件无法读取，请核对配置完整性。", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
