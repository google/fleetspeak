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

"""A setuptools based setup module for Fleetspeak.

Forked from https://github.com/pypa/sampleproject/blob/master/setup.py .
"""
from __future__ import absolute_import
from __future__ import division
from __future__ import print_function
from __future__ import unicode_literals

import io
import os
import shutil
import subprocess

from setuptools import find_packages
from setuptools import setup
from setuptools.command.build_py import build_py
from setuptools.command.sdist import sdist

THIS_DIRECTORY = os.path.dirname(os.path.realpath(__file__))
os.chdir(THIS_DIRECTORY)


with io.open(os.path.join(THIS_DIRECTORY, "VERSION")) as version_file:
  VERSION = version_file.read().strip()


def _CompileProtos():
  """Compiles all Fleetspeak protos."""
  proto_files = []
  for dir_path, _, filenames in os.walk(THIS_DIRECTORY):
    for filename in filenames:
      if filename.endswith(".proto"):
        proto_files.append(os.path.join(dir_path, filename))
  if not proto_files:
    return
  protoc_command = [
      "python", "-m", "grpc_tools.protoc",
      "--python_out", THIS_DIRECTORY,
      "--grpc_python_out", THIS_DIRECTORY,
      "--proto_path", THIS_DIRECTORY,
  ]
  protoc_command.extend(proto_files)
  subprocess.check_output(protoc_command)


class Sdist(sdist):
  """sdist command that explicitly bundles Fleetspeak's VERSION file."""

  def make_release_tree(self, base_dir, files):
    sdist.make_release_tree(self, base_dir, files)

    sdist_version_file = os.path.join(base_dir, "VERSION")
    if os.path.exists(sdist_version_file):
      os.remove(sdist_version_file)
    shutil.copy(os.path.join(THIS_DIRECTORY, "VERSION"), sdist_version_file)


class BuildPy(build_py):
  """Custom build_py command that compiles all Fleetspeak protos."""

  # This is based on grr-response-proto's setup.py.
  def find_all_modules(self):
    _CompileProtos()
    self.packages = find_packages()
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

    # Choose your license
    license="Apache License, Version 2.0",

    # See https://pypi.python.org/pypi?%3Aaction=list_classifiers
    classifiers=[
        # How mature is this project? Common values are
        #   3 - Alpha
        #   4 - Beta
        #   5 - Production/Stable
        "Development Status :: 4 - Beta",

        # Indicate who your project is intended for
        "Intended Audience :: Developers",
        "Topic :: Software Development",

        # Pick your license as you wish (should match "license" above)
        "License :: OSI Approved :: Apache Software License",

        # Specify the Python versions you support here. In particular, ensure
        # that you indicate whether you support Python 2, Python 3 or both.
        "Programming Language :: Python :: 2.7",
    ],

    # What does your project relate to?
    keywords="",

    # You can just specify the packages manually here if your project is
    # simple. Or you can use find_packages().
    packages=find_packages(),

    # Alternatively, if you want to distribute just a my_module.py, uncomment
    # this:
    #   py_modules=["my_module"],

    # List run-time dependencies here.  These will be installed by pip when
    # your project is installed. For an analysis of "install_requires" vs pip"s
    # requirements files see:
    # https://packaging.python.org/en/latest/requirements.html
    install_requires=[
        "absl-py>=0.8.0",
        "grpcio>=1.24.1",
        "grpcio-tools>=1.24.1",
    ],

    # List additional groups of dependencies here (e.g. development
    # dependencies). You can install these using the following syntax,
    # for example:
    # $ pip install -e .[dev,test]
    extras_require={
        "dev": [],
        "test": [],
        ":python_version == '2.7'": [
            "futures>=3.3.0",
        ],
    },

    # If there are data files included in your packages that need to be
    # installed, specify them here.  If using Python 2.6 or less, then these
    # have to be included in MANIFEST.in as well.
    package_data={
    },

    # Although "package_data" is the preferred approach, in some case you may
    # need to place data files outside of your packages. See:
    # http://docs.python.org/3.4/distutils/setupscript.html#installing-additional-files pylint:disable=line-too-long
    # In this case, "data_file" will be installed into "<sys.prefix>/my_data"
    data_files=[str("VERSION")],  # data_files have to be bytes in Python 2.

    # To provide executable scripts, use entry points in preference to the
    # "scripts" keyword. Entry points provide cross-platform support and allow
    # pip to create the appropriate form of executable for the target platform.
    entry_points={
    },

    cmdclass={
        "build_py": BuildPy,
        "sdist": Sdist,
    },
)
