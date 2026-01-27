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
:mod:`supervisor` --- SCION topology supervisor generator
=============================================
"""
# Stdlib
import configparser
import os
import shlex
from io import StringIO

import toml

# SCION
from topology.util import write_file
from topology.common import (
    ArgsTopoDicts,
    DISP_CONFIG_NAME,
    SD_CONFIG_NAME,
    TopoID,
)

SUPERVISOR_CONF = 'supervisord.conf'


class SupervisorGenArgs(ArgsTopoDicts):
    pass


class SupervisorGenerator(object):
    def __init__(self, args):
        """
        :param SupervisorGenArgs args: Contains the passed command line arguments and topo dicts.
        """
        self.args = args

    def generate(self):
        config = configparser.ConfigParser(interpolation=None)

        for topo_id, topo in self.args.topo_dicts.items():
            self._add_as_config(config, topo_id, topo)
        self._add_dispatcher(config)

        self._write_config(config,
                           os.path.join(self.args.output_dir, SUPERVISOR_CONF))

    def _add_as_config(self, config, topo_id, topo):
        entries = self._as_entries(topo_id, topo)
        for elem, entry in sorted(entries):
            self._add_prog(config, elem, entry)
        config["group:as%s" % topo_id.file_fmt()] = {
            "programs": ",".join(name for name, _ in sorted(entries))
        }

    def _as_entries(self, topo_id, topo):
        base = topo_id.base_dir(self.args.output_dir)
        entries = []
        entries.extend(self._br_entries(topo, "bin/router", base))
        entries.extend(self._control_service_entries(topo, base))
        entries.extend(self._sciond_entries(topo_id, topo))
        return entries

    def _br_entries(self, topo, cmd, base):
        entries = []
        for k, v in topo.get("border_routers", {}).items():
            conf = os.path.join(base, "%s.toml" % k)
            prog = self._common_entry(k, [cmd, "--config", conf])
            prog['environment'] += ',GODEBUG="cgocheck=0"'
            entries.append((k, prog))
        return entries

    def _control_service_entries(self, topo, base):
        entries = []
        for k, v in topo.get("control_service", {}).items():
            ia_str = v.get("isd_as")
            if ia_str:
                mem_base = topo.get("membership_base_dirs", {}).get(ia_str, self._as_base_from_ia(ia_str))
                conf = os.path.join(mem_base, "%s.toml" % k)
                # For membership control services, point at their membership sciond/topology.
                sd_path = os.path.join(mem_base, f"sd.{TopoID(ia_str).file_fmt()}.toml")
                sd_addr = self._sd_address(sd_path)
                env = f'SCION_SD_CONFIG="{sd_path}"'
                if sd_addr:
                    env += f',SCION_DAEMON_ADDRESS="{sd_addr}"'
                prog = self._common_entry(k, ["bin/control", "--config", conf])
                prog['environment'] += f",{env}"
            else:
                conf = os.path.join(base, "%s.toml" % k)
                sd_path = os.path.join(base, f"sd.{TopoID(str(topo.get('isd_as', ''))).file_fmt()}.toml")
                sd_addr = self._sd_address(sd_path) if os.path.exists(sd_path) else None
                prog = self._common_entry(k, ["bin/control", "--config", conf])
                if sd_addr:
                    prog['environment'] += f',SCION_DAEMON_ADDRESS="{sd_addr}"'
            entries.append((k, prog))
        return entries

    def _sd_address(self, sd_conf_path):
        try:
            data = toml.load(sd_conf_path)
            return data.get("sd", {}).get("address", "")
        except Exception:
            return ""

    def _sciond_entries(self, topo_id, topo):
        entries = []
        mem_dirs = topo.get("membership_base_dirs", {})
        for ia_str in topo.get("local_ias", [str(topo_id)]):
            mem_id = topo_id if ia_str == str(topo_id) else topo_id.__class__(ia_str)
            sd_name = f"sd.{mem_id.file_fmt()}"
            conf_dir = mem_dirs.get(ia_str, mem_id.base_dir(self.args.output_dir))
            sd_file = f"sd.{mem_id.file_fmt()}.toml"
            cmd_args = [
                "bin/daemon", "--config",
                os.path.join(conf_dir, sd_file)
            ]
            prog = self._common_entry(sd_name, cmd_args)
            entries.append((sd_name, prog))
        return entries

    def _as_base_from_ia(self, ia_str):
        # ia_str is e.g. "25-ff00:0:220"
        from topology.common import TopoID
        mem_id = TopoID(ia_str)
        return mem_id.base_dir(self.args.output_dir)

    def _add_dispatcher(self, config):
        name, entry = self._dispatcher_entry()
        self._add_prog(config, name, entry)

    def _dispatcher_entry(self):
        name = "dispatcher"
        conf_dir = os.path.join(self.args.output_dir, name)
        cmd_args = [
            "bin/dispatcher", "--config",
            os.path.join(conf_dir, DISP_CONFIG_NAME)
        ]
        return (name, self._common_entry(name, cmd_args))

    def _add_prog(self, config, name, entry):
        config["program:%s" % name] = entry

    def _common_entry(self, name, cmd_args):
        entry = {
            'autostart': 'false',
            'autorestart': 'false',
            'environment': 'TZ=UTC',
            'stdout_logfile': "logs/%s.log" % name,
            'redirect_stderr': True,
            'startretries': 0,
            'startsecs': 5,
            'priority': 100,
            'command': ' '.join(shlex.quote(a) for a in cmd_args),
        }
        if name == "dispatcher":
            entry['startsecs'] = 1
            entry['priority'] = 50
        return entry

    def _write_config(self, config, path):
        text = StringIO()
        config.write(text)
        write_file(path, text.getvalue())
