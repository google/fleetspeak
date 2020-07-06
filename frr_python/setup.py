import os
import subprocess
import sys

from setuptools import setup
from setuptools.command.develop import develop

GRPCIO_TOOLS = "grpcio-tools==1.24.1"
THIS_DIRECTORY = os.path.dirname(os.path.realpath(__file__))
os.chdir(THIS_DIRECTORY)

def compile_protos():
    """Compiles all proto files."""
    p = subprocess.Popen([sys.executable, "-m", "grpc_tools.protoc", "--help"],
                         stdout=subprocess.PIPE,
                         stderr=subprocess.PIPE)
    p.communicate()
    if p.returncode != 0:
        subprocess.check_call(
            [sys.executable, "-m", "pip", "install", GRPCIO_TOOLS])

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


class Develop(develop):
    def run(self):
        compile_protos()
        develop.run(self)

setup(
    name="frr_python",
    description="Frr server and client services",
    url="https://github.com/google/fleetspeak/tree/master/frr_python",
    maintainer="GRR Development Team",
    maintainer_email="grr-dev@googlegroups.com",
    license="Apache License, Version 2.0",
    install_requires=[
        "absl-py>=0.8.0",
        "grpcio>=1.24.1",
    ],
    cmdclass={
        "develop": Develop,
    }
)
