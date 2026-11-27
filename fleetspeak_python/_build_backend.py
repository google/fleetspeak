# Copyright 2026 Google Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Custom PEP 517 build backend for fleetspeak.

Compiles `.proto` definitions into `*_pb2.py` and `*_pb2_grpc.py` files using
`grpcio-tools` when generated files are missing or
`FLEETSPEAK_FORCE_COMPILE_PROTOS=1` is set.
"""

from __future__ import annotations

from collections.abc import Iterator, Mapping, Sequence
import contextlib
import importlib.util
import os
import pathlib
import shutil
import subprocess
import sys
from typing import Any

from setuptools import build_meta as _orig

_ROOT = pathlib.Path(__file__).resolve().parent
_PROTO_SUBDIR = pathlib.Path("fleetspeak") / "src"
_PROTO_BUILD_REQUIRES = ("grpcio==1.78.0", "grpcio-tools==1.78.0")


def _resolve_proto_root() -> pathlib.Path | None:
  """Returns the root directory containing `.proto` sources, or `None`.

  Checks `_ROOT` first (flattened codebase archive where `.proto` files sit
  alongside `pyproject.toml`), then `_ROOT.parent` (repository checkout where
  `.proto` files live one level above `fleetspeak_python/`). Returns `None`
  for prebuilt sdists that bundle compiled modules without `.proto` sources.
  """
  for candidate in (_ROOT, _ROOT.parent):
    proto_dir = candidate / _PROTO_SUBDIR
    if proto_dir.is_dir() and any(proto_dir.rglob("*.proto")):
      return candidate
  return None


def _has_missing_generated_files(proto_root: pathlib.Path) -> bool:
  for proto_path in (proto_root / _PROTO_SUBDIR).rglob("*.proto"):
    rel_proto = proto_path.relative_to(proto_root)
    stem = rel_proto.stem.replace("-", "_")
    target = _ROOT / rel_proto
    if not target.with_name(f"{stem}_pb2.py").is_file():
      return True
    if not target.with_name(f"{stem}_pb2_grpc.py").is_file():
      return True
  return False


def _needs_protoc(proto_root: pathlib.Path | None) -> bool:
  """Returns whether `.proto` sources must be compiled."""
  if proto_root is None:
    return False
  return (
      _has_missing_generated_files(proto_root)
      or os.environ.get("FLEETSPEAK_FORCE_COMPILE_PROTOS") == "1"
  )


def _clean_generated_files() -> None:
  target_dir = _ROOT / _PROTO_SUBDIR
  if not target_dir.is_dir():
    return
  for pattern in ("*_pb2.py", "*_pb2.pyc", "*_pb2_grpc.py", "*_pb2_grpc.pyc"):
    for path in target_dir.rglob(pattern):
      path.unlink()


def _compile_protos_if_needed() -> None:
  """Compiles `.proto` files into `*_pb2.py` and `*_pb2_grpc.py` if needed."""
  proto_root = _resolve_proto_root()
  if proto_root is None or not _needs_protoc(proto_root):
    return

  if importlib.util.find_spec("grpc_tools") is None:
    raise RuntimeError(
        "Compiled *_pb2.py / *_pb2_grpc.py files are missing and "
        "grpcio-tools is not available to compile .proto definitions."
    )

  _clean_generated_files()
  (_ROOT / _PROTO_SUBDIR).mkdir(parents=True, exist_ok=True)

  out_dir = _ROOT.relative_to(proto_root).as_posix()
  proto_rel_paths = [
      proto.relative_to(proto_root).as_posix()
      for proto in (proto_root / _PROTO_SUBDIR).rglob("*.proto")
  ]
  subprocess.check_call(
      [
          sys.executable,
          "-m",
          "grpc_tools.protoc",
          "--proto_path=.",
          f"--python_out={out_dir}",
          f"--grpc_python_out={out_dir}",
          *proto_rel_paths,
      ],
      cwd=proto_root,
  )


@contextlib.contextmanager
def _ensure_version_file() -> Iterator[None]:
  """Ensures `VERSION` exists in `_ROOT` while setuptools reads metadata."""
  version_path = _ROOT / "VERSION"
  if version_path.is_file():
    yield
    return

  parent_version = _ROOT.parent / "VERSION"
  if not parent_version.is_file():
    raise RuntimeError(
        f"Could not find VERSION at {version_path} or {parent_version}."
    )

  shutil.copyfile(parent_version, version_path)
  try:
    yield
  finally:
    version_path.unlink(missing_ok=True)


def _extra_proto_requires() -> list[str]:
  if _needs_protoc(_resolve_proto_root()):
    return list(_PROTO_BUILD_REQUIRES)
  return []


def get_requires_for_build_wheel(
    config_settings: Mapping[str, Any] | None = None,
) -> Sequence[str]:
  """Returns build requirements for building a wheel."""
  with _ensure_version_file():
    requires = list(
        _orig.get_requires_for_build_wheel(config_settings=config_settings)
    )
  return [*requires, *_extra_proto_requires()]


def get_requires_for_build_sdist(
    config_settings: Mapping[str, Any] | None = None,
) -> Sequence[str]:
  """Returns build requirements for building a source distribution."""
  with _ensure_version_file():
    requires = list(
        _orig.get_requires_for_build_sdist(config_settings=config_settings)
    )
  return [*requires, *_extra_proto_requires()]


def get_requires_for_build_editable(
    config_settings: Mapping[str, Any] | None = None,
) -> Sequence[str]:
  """Returns build requirements for building an editable wheel."""
  with _ensure_version_file():
    requires = list(
        _orig.get_requires_for_build_editable(config_settings=config_settings)
    )
  return [*requires, *_extra_proto_requires()]


def prepare_metadata_for_build_wheel(
    metadata_directory: str,
    config_settings: Mapping[str, Any] | None = None,
) -> str:
  """Prepares `.dist-info` metadata for building a wheel."""
  with _ensure_version_file():
    _compile_protos_if_needed()
    return _orig.prepare_metadata_for_build_wheel(
        metadata_directory,
        config_settings=config_settings,
    )


def prepare_metadata_for_build_editable(
    metadata_directory: str,
    config_settings: Mapping[str, Any] | None = None,
) -> str:
  """Prepares `.dist-info` metadata for building an editable wheel."""
  with _ensure_version_file():
    _compile_protos_if_needed()
    return _orig.prepare_metadata_for_build_editable(
        metadata_directory,
        config_settings=config_settings,
    )


def build_wheel(
    wheel_directory: str,
    config_settings: Mapping[str, Any] | None = None,
    metadata_directory: str | None = None,
) -> str:
  """Compiles protos if needed and builds a wheel."""
  with _ensure_version_file():
    if metadata_directory is None:
      _compile_protos_if_needed()
    return _orig.build_wheel(
        wheel_directory,
        config_settings=config_settings,
        metadata_directory=metadata_directory,
    )


def build_sdist(
    sdist_directory: str,
    config_settings: Mapping[str, Any] | None = None,
) -> str:
  """Compiles protos if needed and builds a source distribution."""
  with _ensure_version_file():
    _compile_protos_if_needed()
    return _orig.build_sdist(
        sdist_directory,
        config_settings=config_settings,
    )


def build_editable(
    wheel_directory: str,
    config_settings: Mapping[str, Any] | None = None,
    metadata_directory: str | None = None,
) -> str:
  """Compiles protos if needed and builds a PEP 660 editable wheel."""
  with _ensure_version_file():
    if metadata_directory is None:
      _compile_protos_if_needed()
    return _orig.build_editable(
        wheel_directory,
        config_settings=config_settings,
        metadata_directory=metadata_directory,
    )
