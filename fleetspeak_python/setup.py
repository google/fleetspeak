# Copyright 2017 Google Inc.
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

"""A setuptools based setup module for Fleetspeak."""
import os
import shutil
import subprocess
import sys

from setuptools import setup
from setuptools.command.build_py import build_py
from setuptools.command.develop import develop
from setuptools.command.sdist import sdist

GRPCIO_TOOLS = "grpcio-tools==1.24.1"

THIS_DIRECTORY = os.path.dirname(os.path.realpath(__file__))
os.chdir(THIS_DIRECTORY)


def _is_package(path):
  return (os.path.isdir(path) and
          [f for f in os.listdir(path) if f.endswith(".py")])


def _find_packages(path, base="fleetspeak"):
  packages = {}
  for item in os.listdir(path):
    dir = os.path.join(path, item)
    if os.path.isdir(dir):
      if base:
        module_name = "%(base)s.%(item)s" % vars()
      else:
        module_name = item
      packages[module_name] = dir
      packages.update(_find_packages(dir, module_name))

  return {k: v for k, v in packages.items() if _is_package(v)}


def get_version():
  """Get INI parser with version.ini data."""
  ini_path = os.path.join(THIS_DIRECTORY, "VERSION")
  # In a prebuilt sdist archive, version.ini is copied to the root folder
  # of the archive. When installing in a development mode, VERSION
  # has to be read from the root repository folder (two levels above).
  if not os.path.exists(ini_path):
    ini_path = os.path.join(THIS_DIRECTORY, "../VERSION")
    if not os.path.exists(ini_path):
      raise RuntimeError("Couldn't find VERSION")

  with open(ini_path, "r") as fd:
    return fd.read().strip()


VERSION = get_version()


def compile_protos():
  """Builds necessary assets from sources."""
  # Using Popen to effectively suppress the output of the command below - no
  # need to fill in the logs with protoc's help.
  p = subprocess.Popen([sys.executable, "-m", "grpc_tools.protoc", "--help"],
                       stdout=subprocess.PIPE,
                       stderr=subprocess.PIPE)
  p.communicate()
  # If protoc is not installed, install it. This seems to be the only reliable
  # way to make sure that grpcio-tools gets intalled, no matter which Python
  # setup mechanism is used: pip install, pip install -e,
  # python setup.py install, etc.
  if p.returncode != 0:
    # Specifying protobuf dependency right away pins it to the correct
    # version. Otherwise latest protobuf library will be installed with
    # grpcio-tools and then uninstalled when grr-response-proto's setup.py runs
    # and reinstalled to the version required by grr-response-proto.
    subprocess.check_call(
        [sys.executable, "-m", "pip", "install", GRPCIO_TOOLS])

  # If VERSION is present here, we're likely installing from an sdist,
  # so there's no need to compile the protos (they should be already
  # compiled).
  if os.path.exists(os.path.join(THIS_DIRECTORY, "VERSION")):
    return

  proto_files = []
  for dir_path, _, filenames in os.walk(os.path.join(THIS_DIRECTORY, "..")):
    for filename in filenames:
      if filename.endswith(".proto"):
        proto_files.append(os.path.join(dir_path, filename))
  if not proto_files:
    return

  root_dir = os.path.join(THIS_DIRECTORY, "..")
  protoc_command = [
      "python", "-m", "grpc_tools.protoc",
      "--python_out", THIS_DIRECTORY,
      "--grpc_python_out", THIS_DIRECTORY,
      "--proto_path", root_dir,
  ]
  protoc_command.extend(proto_files)
  subprocess.check_call(protoc_command, cwd=root_dir)


class Sdist(sdist):
  """sdist command that explicitly bundles Fleetspeak's VERSION file."""

  def make_release_tree(self, base_dir, files):
    sdist.make_release_tree(self, base_dir, files)

    sdist_version_file = os.path.join(base_dir, "VERSION")
    if os.path.exists(sdist_version_file):
      os.remove(sdist_version_file)
    shutil.copy(os.path.join(THIS_DIRECTORY, "../VERSION"), sdist_version_file)

  def run(self):
    compile_protos()
    sdist.run(self)


class Develop(develop):

  def run(self):
    compile_protos()
    develop.run(self)


class BuildPy(build_py):

  def find_all_modules(self):
    self.packages = _find_packages("fleetspeak")
    return build_py.find_all_modules(self)


setup(
    name="fleetspeak",

    version=VERSION,

    description="Fleetspeak",
    long_description=(
        "Fleetspeak is a framework for communicating with a fleet of "
        "machines, with a focus on security monitoring and basic "
        "administrative use cases. It is a subproject of GRR ( "
        "https://github.com/google/grr/blob/master/README.md ), and can be "
        "seen as an effort to modularizing and modernizing its communication "
        "mechanism."),

    # The project"s main homepage.
    url="https://github.com/google/fleetspeak",

    # Maintainer details.
    maintainer="GRR Development Team",
    maintainer_email="grr-dev@googlegroups.com",
    license="Apache License, Version 2.0",

    # See https://pypi.python.org/pypi?%3Aaction=list_classifiers
    classifiers=[
        "Development Status :: 4 - Beta",
        "Intended Audience :: Developers",
        "Topic :: Software Development",
        "License :: OSI Approved :: Apache Software License",
        "Programming Language :: Python :: 3.6",
    ],

    # What does your project relate to?
    keywords="",

    # Packaging details.
    packages=_find_packages("fleetspeak"),
    install_requires=[
        "absl-py>=0.8.0",
        "grpcio>=1.24.1",
    ],
    package_data={
    },
    data_files=["VERSION"],

    # Commands overrides.
    cmdclass={
        "build_py": BuildPy,
        "develop": Develop,
        "sdist": Sdist,
    },
)
