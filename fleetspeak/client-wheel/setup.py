from optparse import OptionParser
from setuptools import setup
from wheel.bdist_wheel import bdist_wheel

import pathlib
import sys


class BdistWheel(bdist_wheel):
  user_options = [
    ('platform-name=', None, 'Platform name to force.'),
  ]

  def initialize_options(self):
    self.platform_name = None
    bdist_wheel.initialize_options(self)

  def finalize_options(self):
    bdist_wheel.finalize_options(self)
    self.root_is_pure = False

  def get_tag(self):
    impl, abi_tag, plat_name = bdist_wheel.get_tag(self)
    if self.platform_name is not None:
      plat_name = self.platform_name
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
    name="fleetspeak-client-bin",
    version=options.version,
    cmdclass={
        "bdist_wheel": BdistWheel,
    },
    data_files=DataFiles("fleetspeak-client-bin", options.package_root),
)
