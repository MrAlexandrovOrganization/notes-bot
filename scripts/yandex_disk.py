"""Run the legacy foreground client with its existing configuration explicitly."""

import os
from pathlib import Path
import shlex


def arguments(config, home):
    allowed = {"dir", "auth", "exclude-dirs", "proxy", "read-only", "overwrite"}
    values = {}
    for line in config.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        key, separator, raw = line.partition("=")
        key = key.strip()
        if not separator or key not in allowed or key in values:
            raise RuntimeError("Unsupported or duplicate Yandex Disk configuration key")
        tokens = shlex.split(raw, comments=True)
        if len(tokens) != 1:
            raise RuntimeError("Invalid Yandex Disk configuration value")
        values[key] = tokens[0]
    if "dir" not in values:
        raise RuntimeError("Yandex Disk directory must be explicit")
    values.setdefault("auth", str(home / ".config/yandex-disk/passwd"))
    result = ["/usr/bin/yandex-disk", "--no-daemon"]
    for key, value in values.items():
        if key in {"read-only", "overwrite"}:
            if value.lower() in {"yes", "true", "1"}:
                result.append("--" + key)
            elif value.lower() not in {"no", "false", "0"}:
                raise RuntimeError("Invalid Yandex Disk boolean")
        else:
            result.append(f"--{key}={value}")
    return result


if __name__ == "__main__":
    home = Path("/home/maxim")
    argv = arguments(home / ".config/yandex-disk/config.cfg", home)
    os.execv(argv[0], argv)
