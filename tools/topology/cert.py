# Copyright 2014 ETH Zurich
# Copyright 2018 ETH Zurich, Anapaya Systems
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
"""
:mod:`cert` --- SCION topology certificate generator
=============================================
"""
import base64
import collections
import os
from pathlib import Path
import sys

from plumbum import local, CommandNotFound

from topology import common
from topology.util import write_file


class CertGenArgs(common.ArgsTopoConfig):
    pass


class CertGenerator(object):
    def __init__(self, args):
        """
        :param CertGenArgs args: Contains the passed command line
        arguments and the parsed topo config.
        """
        self.args = args
        self.pki = local['./bin/scion-pki']
        if not local.path('./bin/scion-pki').exists():
            try:
                self.pki = local[local.which('scion-pki')]
            except CommandNotFound:
                sys.exit("ERROR: scion-pki executable not found. Run `make` first.")
        self.core_count = collections.defaultdict(int)

    def generate(self, topo_dicts):
        self.pki('testcrypto', '-t', self.args.topo_config, '-o', self.args.output_dir,
                 '--as-validity', '365d')
        self._master_keys(topo_dicts)
        self._copy_files(topo_dicts)

    def _master_keys(self, topo_dicts):
        for topo_id in topo_dicts:
            membership_dirs = topo_dicts[topo_id].get('membership_base_dirs', {})
            targets = [topo_id.base_dir(self.args.output_dir)] + list(membership_dirs.values())
            # Use the same forwarding keys for all memberships of this AS.
            key0 = base64.b64encode(os.urandom(16)).decode()
            key1 = base64.b64encode(os.urandom(16)).decode()
            for target in targets:
                write_file(os.path.join(target, 'keys', 'master0.key'), key0)
                write_file(os.path.join(target, 'keys', 'master1.key'), key1)

    def _copy_files(self, topo_dicts):
        cp = local['cp']
        mkdir = local['mkdir']
        # Copy the certs and key dir for all elements.
        for topo_id, as_topo in topo_dicts.items():
            base = local.path(self.args.output_dir)
            membership_dirs = topo_dicts[topo_id].get('membership_base_dirs', {})
            root_dir = Path(topo_id.base_dir(self.args.output_dir))
            targets = {root_dir: int(topo_id.isd_str())}
            for ia_str, mem_dir in membership_dirs.items():
                targets[Path(mem_dir)] = self._isd_from_dir(mem_dir, topo_id)
            trc_sources = list(base // 'trcs/*.trc')
            # map ISD to issuer IA for membership topo_id
            issuer_map = self._issuer_map_for_memberships(topo_id, topo_dicts)
            for as_dir, isd in targets.items():
                as_dir = Path(as_dir)
                mkdir('-p', as_dir / 'certs')
                # For the root AS dir, TRCs already live in place.
                if trc_sources and as_dir.resolve() != root_dir.resolve():
                    filtered = []
                    for src in trc_sources:
                        dst = as_dir / 'certs' / Path(src).name
                        if Path(src).resolve() == dst.resolve():
                            continue
                        filtered.append(src)
                    if filtered:
                        cp(filtered, as_dir / 'certs/')
                issuer_ia = issuer_map.get(isd)
                issuer_root = Path(self.args.output_dir) / f"AS{issuer_ia.as_file_fmt()}" if issuer_ia else root_dir
                self._copy_isd_crypto(root_dir, issuer_root, as_dir, isd)

    def _copy_isd_crypto(self, as_root: Path, issuer_root: Path, target_dir: Path, isd: int):
        mkdir = local['mkdir']
        cp = local['cp']
        as_root = Path(as_root)
        issuer_root = Path(issuer_root)
        target_dir = Path(target_dir)
        # Copy issuer material unless source==target
        if issuer_root.resolve() != target_dir.resolve():
            crypto_root = issuer_root / 'crypto'
            for sub in ('as', 'ca', 'voting'):
                src_dir = crypto_root / sub
                if not src_dir.is_dir():
                    continue
                dst_dir = target_dir / 'crypto' / sub
                mkdir('-p', dst_dir)
                prefix = f"ISD{isd}-"
                for f in src_dir.iterdir():
                    if f.name.startswith(prefix):
                        cp(str(f), str(dst_dir / f.name))
                    # Do not copy issuer cp-as.key into membership dirs.
        # Ensure the membership AS cert and key are present (from its own AS root).
        as_crypto = as_root / 'crypto' / 'as'
        if as_crypto.is_dir():
            dst_dir = target_dir / 'crypto' / 'as'
            mkdir('-p', dst_dir)
            ia_suffix = as_root.name.split("AS", 1)[-1]
            ia_prefix = f"ISD{isd}-AS{ia_suffix}"
            for f in as_crypto.iterdir():
                if f.name.startswith(ia_prefix):
                    if target_dir.resolve() == as_root.resolve() and (dst_dir / f.name).resolve() == f.resolve():
                        continue
                    cp(str(f), str(dst_dir / f.name))
                elif f.name == 'cp-as.key':
                    if target_dir.resolve() == as_root.resolve() and (dst_dir / f.name).resolve() == f.resolve():
                        continue
                    cp(str(f), str(dst_dir / f.name))
        certs_root = issuer_root / 'certs'
        if certs_root.is_dir():
            dst_dir = target_dir / 'certs'
            mkdir('-p', dst_dir)
            prefix = f"ISD{isd}-"
            for f in certs_root.iterdir():
                if f.name.startswith(prefix):
                    cp(str(f), str(dst_dir / f.name))

    def _isd_from_dir(self, as_dir, topo_id):
        name = os.path.basename(str(as_dir))
        if name.startswith("isd"):
            try:
                return int(name[3:])
            except ValueError:
                pass
        return int(topo_id.isd_str())

    def _issuer_map_for_memberships(self, topo_id, topo_dicts):
        """
        Build a map ISD -> issuer IA for memberships of this topo_id.
        """
        result = {}
        private_isds = topo_dicts[topo_id].get("private_isds", [])
        for entry in private_isds:
            try:
                isd = int(entry.get("isd"))
            except Exception:
                continue
            issuer_str = entry.get("cert_issuer") or str(topo_id)
            result[isd] = topo_id.__class__(issuer_str)
        # base ISD
        result[int(topo_id.isd_str())] = topo_id
        return result
from pathlib import Path
