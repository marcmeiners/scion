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
:mod:`config` --- SCION topology config generator
=============================================
"""
import networkx as nx
# Stdlib
from collections import defaultdict
import configparser
import json
import logging
import os
import sys
import yaml
import shutil
from pathlib import Path
from io import StringIO
from typing import Mapping

# SCION
from python.lib.defines import (
    DEFAULT_MTU,
    DEFAULT6_NETWORK,
    NETWORKS_FILE,
    INTRA_CONFIG_FILE,
    INTRA_TOPOLOGY_FILE,
)
from python.lib.scion_addr import ISD_AS
from python.lib.util import (
    load_yaml_file,
    write_file,
)
from python.topology.cert import CertGenArgs, CertGenerator
from python.topology.common import ArgsBase, TopoID
from python.topology.docker import DockerGenArgs, DockerGenerator
from python.topology.go import GoGenArgs, GoGenerator
from python.topology.jaeger import JaegerGenArgs, JaegerGenerator
from python.topology.net import (
    NetworkDescription,
    IPNetwork,
    SubnetGenerator,
    DEFAULT_NETWORK,
)
from python.topology.prometheus import PrometheusGenArgs, PrometheusGenerator
from python.topology.supervisor import SupervisorGenArgs, SupervisorGenerator
from python.topology.topo import TopoGenArgs, TopoGenerator

DEFAULT_TOPOLOGY_FILE = "topology/default.topo"

SCIOND_ADDRESSES_FILE = "sciond_addresses.json"


class ConfigGenArgs(ArgsBase):
    pass


class ConfigGenerator(object):
    """
    Configuration and/or topology generator.
    """

    def __init__(self, args):
        """
        Initialize an instance of the class ConfigGenerator.

        :param ConfigGenArgs args: Contains the passed command line arguments.
        """
        self.args = args
        self.topo_config = load_yaml_file(self.args.topo_config)
        self.use_intra = self.args.intra_config is not None
        self.intra_config = None
        self.intra_topo_dicts = defaultdict(lambda: None)
        if self.use_intra:
            self.intra_config_file = self.args.intra_config
            self.intra_config = load_yaml_file(self.args.intra_config)
        if self.args.sig and not self.args.docker:
            logging.critical("Cannot use sig without docker!")
            sys.exit(1)
        self.default_mtu = None
        self._read_defaults(self.args.network)

    def _read_defaults(self, network):
        """
        Configure default network.
        """
        network4 = DEFAULT_NETWORK
        network6 = DEFAULT6_NETWORK
        if network and '.' in network:
            network4 = network
        if network and ':' in network:
            network6 = network

        defaults = self.topo_config.get("defaults", {})
        self.subnet_gen4 = SubnetGenerator(network4, self.args.docker)
        self.subnet_gen6 = SubnetGenerator(network6, self.args.docker)
        self.default_mtu = defaults.get("mtu", DEFAULT_MTU)

    def generate_all(self):
        """
        Generate all needed files.
        """
        self._ensure_uniq_ases()
        if self.use_intra:
            self._ensure_correct_format()
        topo_dicts, self.all_networks = self._generate_topology()
        self.networks = remove_v4_nets(self.all_networks)
        self._generate_with_topo(topo_dicts)
        self._write_networks_conf(self.networks, NETWORKS_FILE)
        self._write_sciond_conf(self.networks, SCIOND_ADDRESSES_FILE)
        if self.use_intra:
            self._write_intra_files()

    def remove_unused_BR(self, intra_topo_dict, asStr):

        BR_used = list(self.intra_config["ASes"][asStr]['Borderrouter'].keys())
        intra_topo_dict['Nodes']['Borderrouter'] = [
            BR for BR in intra_topo_dict['Nodes']['Borderrouter'] if BR in BR_used]
        all_nodes = []
        for _, node_list in intra_topo_dict['Nodes'].items():
            all_nodes.extend(node_list)

        for link in list(intra_topo_dict['links']):
            if link['a'] not in all_nodes or link['b'] not in all_nodes:
                intra_topo_dict['links'].remove(link)

        return intra_topo_dict

    def _ensure_correct_format(self):
        # we know that AS are unique
        self.check_IP_version()
        self.check_AS_internal_topology()

        for asStr, config in self.intra_config["ASes"].items():
            intra_topo_file = Path(config["Intra-Topology"])
            borderrouters = config["Borderrouter"]
            self.check_all_BR_defined(asStr, borderrouters)
            if not intra_topo_file.is_absolute():
                intra_config_folder = Path(self.intra_config_file).parent
                intra_topo_file = Path(intra_config_folder, intra_topo_file)

            if not intra_topo_file.is_file():
                logging.critical(
                    "ERROR for AS %s: Intra topology file '%s' not found", asStr, intra_topo_file)
                sys.exit(1)

            intra_topo_dict = load_yaml_file(intra_topo_file)

            self.check_file_format(intra_topo_dict, asStr)

            self.check_node_naming(intra_topo_dict, borderrouters, asStr)

            intra_topo_dict = self.remove_unused_BR(intra_topo_dict, asStr)
            self.intra_topo_dicts[asStr] = intra_topo_dict
            # check if links have start + end node that are defined in the intra topology
            self.check_links(intra_topo_dict, asStr)
            # now check if SCION nodes only have 1 internal connection
            self.check_NR_connections(intra_topo_dict, asStr)
            # check if network is connected
            self.check_network_connected(intra_topo_dict, asStr)
            #check if colibri paths have correct format
            if('colibri-paths' in intra_topo_dict):
                self.check_colibri_paths(intra_topo_dict)
            
    def check_colibri_paths(self, intra_topo_dict):
        # Build a graph to check connectivity of colibri paths
        nodes = intra_topo_dict["Nodes"]
        G = nx.MultiGraph()
        for node_type, node_list in nodes.items():
            for node in node_list:
                G.add_node(node)
        topo_links = intra_topo_dict["links"]
        for link in topo_links:
            a = link['a']
            b = link['b']
            G.add_edge(a, b)
        # Paths have to be of minimum length 3
        intra_topo_dict['colibri-paths'] = [path for path in intra_topo_dict['colibri-paths'] if len(path) >= 3]
        borderrouters = list(intra_topo_dict['Nodes']['Borderrouter'])
        borderrouter_pairs = {}
        for path in list(intra_topo_dict['colibri-paths']):
            # First and last node must be border routers
            if not (path[0] in borderrouters and path[-1] in borderrouters):
                intra_topo_dict['colibri-paths'].remove(path)
            # No loops allowed in a colibri path
            if len(path) > len(set(path)):
                intra_topo_dict['colibri-paths'].remove(path)
            # Check connectivity of colibri paths
            for node1, node2 in zip(path, path[1:]):
                if not G.has_edge(node1, node2):
                    intra_topo_dict['colibri-paths'].remove(path)
                    break
            # Only two paths allowed per (ordered) border router pair (paths are directed)
            if (path[0], path[-1]) in borderrouter_pairs.keys():
                if borderrouter_pairs[(path[0], path[-1])] == 2:
                    intra_topo_dict['colibri-paths'].remove(path)
                else:
                    borderrouter_pairs[(path[0], path[-1])] = 2
            else:
                borderrouter_pairs[(path[0], path[-1])] = 1

    def check_IP_version(self):
        for asStr, params in self.topo_config["ASes"].items():
            underlay = params.get("underlay", "")
            if "IPv6" in underlay:
                logging.critical("ERROR: AS %s: IPv6 currently not supported", asStr)
                sys.exit(1)

    def check_AS_internal_topology(self):
        ASes = set()
        for asStr in self.topo_config["ASes"]:
            ASes.add(asStr)

        for asStr in self.intra_config["ASes"]:
            if asStr not in ASes:
                logging.critical("AS '%s' not found in topology", asStr)
                sys.exit(1)
            ASes.remove(asStr)

        if len(ASes) != 0:
            logging.critical("Not all ASes defined in intra topology: %s", ASes)
            sys.exit(1)

    def check_all_BR_defined(self, asStr, borderrouters):
        inter_AS_links = self.topo_config.get('links', {})
        SCION_BRs = set()
        for link in inter_AS_links:
            a = link['a']
            b = link['b']

            for x in [a, b]:
                if x.startswith(asStr):
                    x_no_itf = x.split('#')[0]
                    x_split = x_no_itf.split('-')
                    if len(x_split) == 3:
                        # specific ID is given
                        # don't save interface, because this BR will have multiple interfaces
                        SCION_BRs.add(x_no_itf)
                    else:
                        if x in SCION_BRs:
                            logging.critical(
                                f"AS {asStr}: Borderrouter '{x}' is defined multiple times. " +
                                "Add unique interface to its name.")
                            sys.exit(1)
                        # no specific ID is given, save all interfaces
                        SCION_BRs.add(x)

        for internal_name, topo_name in borderrouters.items():
            if topo_name not in SCION_BRs:
                logging.critical(
                    "ERROR: AS %s: Borderrouter '%s' not found in topology", asStr, topo_name)
                sys.exit(1)
            SCION_BRs.remove(topo_name)

        if len(SCION_BRs) != 0:
            logging.critical(
                "ERROR: AS %s: Not all Borderrouters defined in intra topology: %s",
                asStr, SCION_BRs)
            sys.exit(1)

    def check_file_format(self, intra_topo_dict, asStr):
        nodes = intra_topo_dict.get('Nodes')
        possible_categories = set(['Colibri', 'Control-Service', 'SCION-Daemon',
                                  'Borderrouter', 'Client', 'Internal-Router'])
        actual_categories = set(nodes.keys())
        if possible_categories != actual_categories:
            logging.critical(
                "ERROR: AS %s: Wrong categories or not all categories defined in intra topology.\
                \nPossible categories are %s", asStr, possible_categories)
            sys.exit(1)

        for node_type, node_list in nodes.items():
            node_list = [node for node in node_list if node is not None]
            if not node_list or len(node_list) == 0:
                logging.critical(
                    "ERROR: AS %s: No node defined in category: '%s', in intra topology",
                    asStr, node_type)
                sys.exit(1)

            if node_type == 'Colibri' and len(node_list) > 1:
                logging.critical("ERROR: AS %s: More than one Colibri defined in intra topology",
                                 asStr)
                sys.exit(1)
            if node_type == 'Control-Service' and len(node_list) > 1:
                logging.critical(
                    "ERROR: AS %s: More than one Control-Service defined in intra topology", asStr)
                sys.exit(1)

    def check_node_naming(self, intra_topo_dict, borderrouters, asStr):
        nodes = intra_topo_dict["Nodes"]
        seen = set()
        topo_BR = []
        for node_type, node_list in nodes.items():
            if node_type == "Borderrouter":
                topo_BR = node_list
            for node in node_list:
                if type(node) != str:
                    logging.critical("ERROR: AS %s: Node name is not a string: %s", asStr, node)
                    sys.exit(1)
                if len(node) > 8:
                    logging.critical("ERROR: AS %s: Node name '%s' is too long", asStr, node)
                    sys.exit(1)
                if node in seen:
                    logging.critical(
                        "ERROR: AS %s: Non-unique Node name '%s' in intra topology", asStr, node)
                    sys.exit(1)
                seen.add(node)
        # check if borderrouters in intra.config are a sublist of this node_list
        if not (set(borderrouters.keys()) <= set(topo_BR)):
            logging.critical(
                "ERROR: AS %s: Not all Borderrouter names defined in intra topology!", asStr)
            sys.exit(1)

    def check_link_properties(self, link, asStr):
        bw = link.get('bw', None)
        delay = link.get('delay', None)
        jitter = link.get('jitter', None)
        loss = link.get('loss', None)
        for metric, value in [('bw', bw), ('delay', delay), ('jitter', jitter)]:
            if value is None:
                continue
            try:
                int(value)
            except ValueError:
                logging.critical(
                    "ERROR: AS %s: %s value '%s' is not an integer", asStr, metric, value)
                sys.exit(1)
        if loss is not None:
            try:
                float(loss)
            except ValueError:
                logging.critical(
                    "ERROR: AS %s: Loss value '%s' is not a float", asStr, loss)
                sys.exit(1)

    def check_links(self, intra_topo_dict, asStr):
        nodes = intra_topo_dict["Nodes"]
        node_names = []
        for node_type, node_list in nodes.items():
            node_names.extend(node_list)

        links = intra_topo_dict["links"]
        for link in links:
            a = link['a']
            b = link['b']
            if a not in node_names:
                logging.critical(
                    "ERROR: AS %s: Node '%s' not found in intra topology, but used in links",
                    asStr, a)
                sys.exit(1)
            if b not in node_names:
                logging.critical(
                    "ERROR: AS %s: Node '%s' not found in intra topology, but used in links",
                    asStr, b)
                sys.exit(1)

            # checking if properties are correctly defined
            self.check_link_properties(link, asStr)

    def check_NR_connections(self, intra_topo_dict, asStr):
        nodes = intra_topo_dict["Nodes"]
        NR_connections = {}
        for node_type, node_list in nodes.items():
            for node in node_list:
                NR_connections[node] = 0

        topo_links = intra_topo_dict["links"]
        for link in topo_links:
            a = link['a']
            b = link['b']

            if a not in nodes["Internal-Router"] and b not in nodes["Internal-Router"]:
                logging.critical(
                    'ERROR: AS %s: Link between %s and %s is not allowed in intra topology, \
                        use internal-router to connect them.', asStr, a, b)
                sys.exit(1)

            NR_connections[a] += 1
            NR_connections[b] += 1

        for node, total_links in NR_connections.items():
            if total_links > 1 and node not in nodes["Internal-Router"]:
                logging.critical(
                    "ERROR: AS %s: Node '%s' can't have more than 1 internal link", asStr, node)
                sys.exit(1)

            if total_links == 0:
                logging.critical(
                    "ERROR: AS %s: Node '%s' not allow to have 0 internal links", asStr, node)
                sys.exit(1)

    def check_network_connected(self, intra_topo_dict, asStr):
        nodes = intra_topo_dict["Nodes"]
        G = nx.MultiGraph()
        for node_type, node_list in nodes.items():
            for node in node_list:
                G.add_node(node)
        topo_links = intra_topo_dict["links"]
        for link in topo_links:
            a = link['a']
            b = link['b']
            G.add_edge(a, b)
        if not nx.is_connected(G):
            logging.critical("ERROR: AS %s: Intra topology is not connected", asStr)
            sys.exit(1)

    def _ensure_uniq_ases(self):
        seen = set()
        for asStr in self.topo_config["ASes"]:
            ia = ISD_AS(asStr)
            if ia.as_str() in seen:
                logging.critical("Non-unique AS Id '%s'", ia.as_str())
                sys.exit(1)
            seen.add(ia.as_str())

    def _generate_with_topo(self, topo_dicts):
        self._generate_go(topo_dicts)
        if self.args.docker:
            self._generate_docker(topo_dicts)
        else:
            self._generate_supervisor(topo_dicts)
        self._generate_jaeger(topo_dicts)
        self._generate_prom_conf(topo_dicts)
        self._generate_certs_trcs(topo_dicts)

    def _generate_certs_trcs(self, topo_dicts):
        certgen = CertGenerator(self._cert_args())
        certgen.generate(topo_dicts)

    def _cert_args(self):
        return CertGenArgs(self.args, self.topo_config)

    def _generate_go(self, topo_dicts):
        args = self._go_args(topo_dicts)
        go_gen = GoGenerator(args)
        go_gen.generate_br()
        go_gen.generate_sciond()
        go_gen.generate_control_service()
        go_gen.generate_co()
        go_gen.generate_disp()

    def _go_args(self, topo_dicts):
        return GoGenArgs(self.args, topo_dicts, self.networks)

    def _generate_jaeger(self, topo_dicts):
        args = JaegerGenArgs(self.args, topo_dicts)
        jaeger_gen = JaegerGenerator(args)
        jaeger_gen.generate()

    def _generate_topology(self):
        topo_gen = TopoGenerator(self._topo_args())
        return topo_gen.generate()

    def _topo_args(self):
        return TopoGenArgs(self.args, self.topo_config, self.intra_config, self.intra_topo_dicts,
                           self.subnet_gen4, self.subnet_gen6, self.default_mtu)

    def _generate_supervisor(self, topo_dicts):
        args = self._supervisor_args(topo_dicts)
        super_gen = SupervisorGenerator(args)
        super_gen.generate()

    def _supervisor_args(self, topo_dicts):
        return SupervisorGenArgs(self.args, topo_dicts)

    def _generate_docker(self, topo_dicts):
        args = self._docker_args(topo_dicts)
        docker_gen = DockerGenerator(args)
        docker_gen.generate()

    def _docker_args(self, topo_dicts):
        return DockerGenArgs(self.args, topo_dicts, self.all_networks)

    def _generate_prom_conf(self, topo_dicts):
        args = self._prometheus_args(topo_dicts)
        prom_gen = PrometheusGenerator(args)
        prom_gen.generate()

    def _prometheus_args(self, topo_dicts):
        return PrometheusGenArgs(self.args, topo_dicts, self.networks)

    def _write_ca_files(self, topo_dicts, ca_files):
        isds = set()
        for topo_id, as_topo in topo_dicts.items():
            isds.add(topo_id[0])
        for isd in isds:
            base = os.path.join(self.args.output_dir, "CAS")
            for path, value in ca_files[int(isd)].items():
                write_file(os.path.join(base, path), value.decode())

    def _write_networks_conf(self,
                             networks: Mapping[IPNetwork, NetworkDescription],
                             out_file: str):
        config = configparser.ConfigParser(interpolation=None)
        for net, net_desc in networks.items():
            sub_conf = {}
            for prog, ip_net in net_desc.ip_net.items():
                sub_conf[prog] = str(ip_net.ip)
            config[str(net)] = sub_conf
        text = StringIO()
        config.write(text)
        write_file(os.path.join(self.args.output_dir, out_file), text.getvalue())

    def _write_sciond_conf(self, networks: Mapping[IPNetwork, NetworkDescription], out_file: str):
        d = dict()
        for net_desc in networks.values():
            for prog, ip_net in net_desc.ip_net.items():
                if prog.startswith("sd"):
                    ia = prog[2:].replace("_", ":")
                    d[ia] = str(ip_net.ip)
        with open(os.path.join(self.args.output_dir, out_file), mode="w") as f:
            json.dump(d, f, sort_keys=True, indent=4)

    def _write_intra_files(self):
        new_intra_config = self.intra_config
        for asStr, config in self.intra_config["ASes"].items():
            intra_topo_file = Path(config["Intra-Topology"])
            if not intra_topo_file.is_absolute():
                intra_config_folder = Path(self.intra_config_file).parent
                intra_topo_file = Path(intra_config_folder, intra_topo_file)

            AS_dir = TopoID(asStr).AS_file()
            new_intra_file_path = Path(AS_dir, INTRA_TOPOLOGY_FILE)

            Path(AS_dir).mkdir(parents=True, exist_ok=True)
            shutil.copyfile(intra_topo_file,
                            Path(self.args.output_dir, new_intra_file_path))

            new_intra_config["ASes"][asStr]["Intra-Topology"] = str(new_intra_file_path)

        outfile = Path(self.args.output_dir, INTRA_CONFIG_FILE)
        write_file(str(outfile), yaml.dump(new_intra_config, default_flow_style=False))


def remove_v4_nets(nets: Mapping[IPNetwork, NetworkDescription]
                   ) -> Mapping[IPNetwork, NetworkDescription]:
    res = {}
    for net, net_desc in nets.items():
        if net_desc.name.endswith('_v4'):
            continue
        res[net] = net_desc
    return res
