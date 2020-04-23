from optparse import OptionParser
from setuptools import setup
from wheel.bdist_wheel import bdist_wheel

import pathlib
import sys


class BdistWheel(bdist_wheel):

  def finalize_options(self):
    bdist_wheel.finalize_options(self)
    self.root_is_pure = False

  def get_tag(self):
    impl, abi_tag, plat_name = bdist_wheel.get_tag(self)
    if plat_name == "linux_x86_64":
      # pypi doesn't support linux_x86_64
      # HTTPError: 400 Client Error: Binary wheel
      # 'fleetspeak_server_bin-0.1.7-py2.py3-none-linux_x86_64.whl'
      # has an unsupported platform tag 'linux_x86_64'.
      plat_name = "manylinux2010_x86_64"
    return "py2.py3", "none", plat_name


def GetOptions():
  parser = OptionParser()
  parser.add_option("--package-root")
  parser.add_option("--version")
  options, sys.argv[1:] = parser.parse_args()
  for option in "package_root", "version":
    if not getattr(options, option):
      parser.error("--{} is required.".format(option))
  return options


def DataFiles(prefix_in_pkg, root_dir):
  result = []
  for path in pathlib.Path(root_dir).glob("**/*"):
    if path.is_dir():
      continue
    relative = path.relative_to(root_dir)
    result.append((str(prefix_in_pkg / relative.parent), [str(path)]))
  return result


options = GetOptions()

setup(
    name="fleetspeak-server-bin",
    version=options.version,
    cmdclass={
        "bdist_wheel": BdistWheel,
    },
    data_files=DataFiles("fleetspeak-server-bin", options.package_root),
)
