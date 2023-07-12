#!/usr/bin/env python3

import argparse
import os
import json
import sys
import configparser
import networkx as nx

from AutonomousSystem import AutonomousSystem
from pathlib import Path
from CLI import AS_CLI
from mininet.log import info, output, error
from intra_colibri import IntraColibri
from path_selection import ComputeColibriPaths

from python.topology.common import TopoID

from python.lib.util import (
    load_yaml_file,
)
from python.lib.defines import (
    GEN_PATH,
    NETWORKS_FILE,
    IFIDS_FILE,
    AS_LIST_FILE,
    TOPO_FILE,
    INTRA_CONFIG_FILE,
)


class SCIONTopology(object):
    """Creates multiple ASes and connects them to each other"""

    def __init__(self, scion_apps_path, gen_path):
        """Initialize the topology"""
        self.SCION_PATH = Path().absolute()
        self.SCION_APPS_PATH = scion_apps_path
        self.gen_dir = Path(gen_path)
        # using getlogin() returning username
        self.username = os.getlogin()
        if self.username == 'root':
            output("You are root. Please run this script (+SCION) as a regular user.")
            sys.exit(1)

        self.networks_file = Path(self.gen_dir, NETWORKS_FILE)
        self.AS_list_file = Path(self.gen_dir, AS_LIST_FILE)
        self.interface_IDS_file = Path(self.gen_dir, IFIDS_FILE)
        self.intra_config = Path(self.gen_dir, INTRA_CONFIG_FILE)

        self.AS_yaml_dict = load_yaml_file(self.AS_list_file)
        self.intra_config_dict = load_yaml_file(self.intra_config)
        self.ifids_dict = load_yaml_file(self.interface_IDS_file)

        self.networks_config = configparser.ConfigParser()
        self.networks_config.read(self.networks_file)

        self.ASes = {}
        self.initialize_ASes()
        self.networks = {}
        self.BR_name_map = {}

    def initialize_ASes(self):
        """For each AS define some useful attributes"""
        for _, value_list in self.AS_yaml_dict.items():
            for ISD_AS_id in value_list:
                # replace to create AS name like in the networks file
                topo_id = TopoID(ISD_AS_id)
                AS_config_dict = self.intra_config_dict["ASes"][ISD_AS_id]
                intra_topo = AS_config_dict['Intra-Topology']
                p = Path(intra_topo)
                if not p.is_absolute():
                    config_dir = self.intra_config.parent
                    p = Path(config_dir, intra_topo)

                intra_topo_dict = self.gen_intra_topo_dict(
                    p, AS_config_dict['Borderrouter'])
                graph = self.gen_intra_topo_graph(intra_topo_dict, ISD_AS_id)

                self.ASes[ISD_AS_id] = {
                    "FULL_NAME": topo_id.file_fmt(),
                    'AS_gen_path': topo_id.base_dir(self.gen_dir),
                    "intra_topology_dict": intra_topo_dict,
                    "routing_protocol": AS_config_dict['Routing-Protocol'],
                    "BR_name_mapping": AS_config_dict['Borderrouter'],
                    'intra_topo_graph': graph
                }

    def gen_intra_topo_dict(self, intra_topology, BR_name_mapping):
        """Generate intra topology dict from yaml file and remove unused BRs (+ their links)"""
        intra_topo_dict = load_yaml_file(intra_topology)

        BR_used = list(BR_name_mapping.keys())
        intra_topo_dict['Nodes']['Borderrouter'] = [
            BR for BR in intra_topo_dict['Nodes']['Borderrouter'] if BR in BR_used]
        all_nodes = []
        for _, node_list in intra_topo_dict['Nodes'].items():
            all_nodes.extend(node_list)

        for link in list(intra_topo_dict['links']):
            if link['a'] not in all_nodes or link['b'] not in all_nodes:
                intra_topo_dict['links'].remove(link)
        if 'colibri-paths' in list(intra_topo_dict):
            for path in intra_topo_dict['colibri-paths']:
                colibri_path = list(path)
                border_routers = list(intra_topo_dict['Nodes']['Borderrouter'])
                assert len(colibri_path) > 1, "a colibri path must be of minimum lentgh 2"
                assert colibri_path[0] in border_routers and colibri_path[-1] in border_routers, "Paths must start and end with a border router"         
                # TODO: Add condition: paths with same start and end router must contain distinct non-br-nodes

        return intra_topo_dict

    def gen_intra_topo_graph(self, intra_topo_dict, ISD_AS_id):
        """Generate intra topology graph from the intra topology dict"""
        G = nx.MultiGraph()
        nodes = intra_topo_dict["Nodes"]
        for _, node_list in nodes.items():
            for node in node_list:
                G.add_node(node)
        topo_links = intra_topo_dict["links"]
        for link in topo_links:
            a = link['a']
            b = link['b']
            label = ""
            if link.get('bw', None) is not None:
                label += f'BW: {link["bw"]}Mbit/s\n'
            if link.get('delay', None) is not None:
                label += f'Delay: {link["delay"]}ms\n'
            if link.get('loss', None) is not None:
                label += f'Loss: {link["loss"]}%\n'
            if link.get('jitter', None) is not None:
                label += f'Jitter: {link["jitter"]}ms\n'
            if link.get('mtu', None) is not None:
                label += f'MTU: {link["mtu"]}B\n'
            label = label.strip()
            G.add_edge(a, b, label=label)
        if not nx.is_connected(G):
            error("ERROR: AS %s: Intra topology is not connected", ISD_AS_id)
            sys.exit(1)   
        return G

    def create_networks(self):
        """Create Mininet networks for each AS

        Store Mininet network reference + other useful attributes in self.networks
        """
        for ISD_AS_id, AS_config in self.ASes.items():
            AS = AutonomousSystem(
                self, ISD_AS_id=ISD_AS_id, **AS_config)
            AS.build()
            # get real BR, not only its string name
            BRs = [AS.net.get(BR)
                   for BR in AS_config['BR_name_mapping'].keys()]

            AS_gen_path = AS_config['AS_gen_path']
            with open(Path(AS_gen_path, TOPO_FILE)) as f:
                topology_dict = json.load(f)
            self.networks[ISD_AS_id] = {
                "BRs": BRs,
                'AS': AS,
                'topology_dict': topology_dict,
                'graph': AS_config['intra_topo_graph']
            }

    def add_inter_AS_links(self, ISD_AS_id, BR_dict, links_created):
        """Add links between ASes

        Add also link properties defined in the SCION topology file
        and now stored in the topology dict (from topology.json)
        """
        BR_SUBNETS = self.networks[ISD_AS_id]['AS'].BR_SUBNETS
        border_routers = self.networks[ISD_AS_id]['topology_dict']['border_routers']

        for br_full_name in border_routers:
            # for each border router in this isd-AS we create the link associated to the interface
            BR1_name, in_net_BR1_name = br_full_name.split('-@')

            for BR1_ifid, intf_info in border_routers[br_full_name]['interfaces'].items():
                local_ip = intf_info['underlay']['public'].split(':')[0]
                remote_ip = intf_info['underlay']['remote'].split(':')[0]
                addr1 = f'{local_ip}/31'
                addr2 = f'{remote_ip}/31'
                AS1 = ISD_AS_id
                AS2 = intf_info['isd_as']

                good_subnet = None
                for subnet in BR_SUBNETS:
                    # we already have the IP -> only one subnet that contains this address
                    for name, ip in self.networks_config[subnet].items():
                        if ip == local_ip:
                            good_subnet = subnet
                            break
                if good_subnet is None:
                    raise Exception(f'ERROR: Could not find subnet with IP: {local_ip}')

                in_net_BR2_name = None
                # good subnet must contain the remote IP
                for br_name, ip in self.networks_config[good_subnet].items():
                    if ip == remote_ip:
                        in_net_BR2_name = br_name.split('@')[1]
                        break
                if in_net_BR2_name is None:
                    raise Exception(
                        f'ERROR: Could not corresponding Borderrouter with IP: {remote_ip}')

                # extract correct interface
                BR2_name, BR2_ifid = BR_dict[f'{BR1_name} {BR1_ifid}'].split()

                # retrieve right BR from the Mininet network
                in_net_BR1, in_net_BR2 = None, None
                for BR in self.networks[AS1]['BRs']:
                    if BR.name == in_net_BR1_name:
                        in_net_BR1 = BR
                for BR in self.networks[AS2]['BRs']:
                    if BR.name == in_net_BR2_name:
                        in_net_BR2 = BR
                if in_net_BR1 is None or in_net_BR2 is None:
                    raise Exception(
                        f'ERROR: BR not found in network: {in_net_BR1_name} or {in_net_BR2_name}')

                # create actual link (virtual ethernet link)
                intf1 = f'veth-{BR1_ifid}'
                intf2 = f'veth-{BR2_ifid}'
                in_net_BR1.cmd(
                    f'ip link add {intf1} type veth peer name {intf2} netns {in_net_BR2.pid}')
                in_net_BR1.cmd(f'ifconfig {intf1} {addr1}')
                in_net_BR2.cmd(f'ifconfig {intf2} {addr2}')

                # add defined link properties
                self.add_link_attributes(in_net_BR1, in_net_BR2, intf1, intf2,
                                         mtu=intf_info['mtu'],  # mtu always set
                                         bw=intf_info.get('bw', None),
                                         delay=intf_info.get('delay', None),
                                         jitter=intf_info.get('jitter', None),
                                         loss=intf_info.get('loss', None))

                links_created.add(f'{BR1_name} {BR1_ifid}')
                links_created.add(f'{BR2_name} {BR2_ifid}')
            return links_created

    def add_link_attributes(self, in_net_BR1, in_net_BR2, intf1, intf2,
                            mtu, bw, delay, jitter, loss):
        """Add link properties defined in the SCION topology file

        Adapted from Mininets traffic control implementation: mininet/link.py TCIntf
        """
        in_net_BR1.cmd(f'ip link set dev {intf1} mtu {mtu}')
        in_net_BR2.cmd(f'ip link set dev {intf2} mtu {mtu}')
        parent = ' root '
        cmds = []
        if bw and (bw < 0 or bw > 1000):
            error(
                f'Bandwidth limit {bw} is outside supported range 0..1000 - ignoring\n')
        elif bw:
            cmds += ['tc qdisc add dev %s root handle 5:0 htb default 1',
                     'tc class add dev %s parent 5:0 classid 5:1 htb ' +
                     f'rate {bw}Mbit burst 15k']
            parent = ' parent 5:1 '

        if loss and (loss < 0 or loss > 100):
            error(f'Bad loss percentage {loss}\n')

        netemargs = '%s%s%s' % (
            f'delay {delay}ms ' if delay is not None else '',
            f'{jitter}ms ' if jitter is not None else '',
            f'loss {loss:.5f} ' if (loss is not None and loss > 0) else '')
        if netemargs:
            cmds += ['tc qdisc add dev %s ' +
                     f'{parent} handle 10: netem {netemargs}']
        # apply commands to both interfaces
        for cmd in cmds:
            in_net_BR1.cmd(cmd % intf1)
            in_net_BR2.cmd(cmd % intf2)
            
    def initiate_colibri_paths(self, ISD_AS_id, AS):
        intra_co = IntraColibri(AS)
        # If Colibri paths are specified in the topology file, use them, otherwise compute them
        if 'colibri-paths' in list(self.ASes[ISD_AS_id]["intra_topology_dict"]):
            label = 100
            br_pair_to_path = {}
            for path in list(self.ASes[ISD_AS_id]["intra_topology_dict"]['colibri-paths']):
                if (path[0], path[-1]) in br_pair_to_path.keys():
                    if(path != br_pair_to_path[(path[0], path[-1])]):
                        intra_co.initiatePath(path, label, "0x11")
                else:
                    intra_co.initiatePath(path, label, "0x10")
                    br_pair_to_path[(path[0], path[-1])] = path
                label += 1
        else:
            border_routers = list(self.ASes[ISD_AS_id]['intra_topology_dict']['Nodes']['Borderrouter'])
            graph = self.ASes[ISD_AS_id]['intra_topo_graph']
            pairs = list(set([(min(i,j), max(i,j)) for i in border_routers for j in border_routers if i!=j]))
            pairs += list(set([(max(i,j), min(i,j)) for i in border_routers for j in border_routers if i!=j]))
            diGraph = nx.DiGraph(graph)
            br_pair_to_paths = {}
            demands = {}
            number_of_random_samples = 10
            path_selection = ComputeColibriPaths()
            for (i,j) in pairs:
                paths_nodes = list(nx.all_simple_paths(diGraph, source=i, target=j))
                paths_links = []
                for p in paths_nodes:
                    paths_links.append([(p[k], p[k + 1]) for k in range(len(p) - 1)])
                br_pair_to_paths[(i,j)] = paths_links
                demands[(i,j)] = 0
            br_pair_to_chosen_paths, edge_usages = path_selection.choose_paths(br_pair_to_paths, diGraph, None,  number_of_random_samples, demands)
            br_pair_to_chosen_paths, edge_usages = path_selection.choose_backup_paths(br_pair_to_paths, br_pair_to_chosen_paths, diGraph, edge_usages, number_of_random_samples, demands)
            label = 100
            for paths in br_pair_to_chosen_paths.values():
                path1 = path_selection.path_as_node_list(paths[0])
                intra_co.initiatePath(path1, label, "0x10")
                label += 1
                path2 = path_selection.path_as_node_list(paths[1])
                if path1 != path2:
                    intra_co.initiatePath(path1, label, "0x11")
                    label += 1
                            
    def start_CLI(self):
        """Start global CLI"""
        while True:
            try:
                output('\n')
                output('#################################################\n')
                output('##### Which network do you want to inspect? #####\n')
                output('[0] Shutdown topology\n')
                network_list = list(self.networks.keys())
                for i, network in enumerate(network_list):
                    print(f'[{i+1}] {network}')
                try:
                    network_index = int(input('Network index: ').strip())
                except Exception:
                    print('Invalid network index')
                    continue
                if network_index == 0:
                    break
                if network_index > len(network_list):
                    print('Network index to big')
                    continue
                network = network_list[network_index-1]
                AS = self.networks[network]['AS']
                # start CLI for specific AS
                AS_CLI(networks=self.networks,
                       ASes=self.ASes,
                       AS=AS
                       )
            except (Exception, KeyboardInterrupt):
                pass

    def start(self):
        """After everything is set up, start all ASes"""
        # create individual ASes
        output('------   Creating ASes...   ------\n')
        self.create_networks()
        # create intra-AS links
        output('------   Connect ASes with inter-AS links...   ------\n')
        links_created = set()
        for ISD_AS_id, BR_dict in self.ifids_dict.items():
            links_created = self.add_inter_AS_links(
                ISD_AS_id, BR_dict, links_created)
        output('------   Starting ASes...   ------\n')
        for ISD_AS_id, AS_dict in self.networks.items():
            AS = AS_dict['AS']
            AS.add_SCION_services()
            AS.start()
            self.initiate_colibri_paths(ISD_AS_id, AS)
            output(f'------   Network {ISD_AS_id} started ------\n')

        info('\n#####         All Networks started!         #####')
        self.start_CLI()
        self.stop()

    def stop(self):
        """Shutdown everything"""
        output('\n------   Stopping Networks...   ------\n')
        for ISD_AS_id, AS_dict in self.networks.items():
            output(f'------   Stopping network {ISD_AS_id}   ------\n')
            AS = AS_dict['AS']
            AS.stop()


def check_scion_apps(apps_argument):
    """Check if the scion apps are cloned and available"""

    if apps_argument is None:
        print('SCION_APPS_PATH is not set')
        print('Either set it as environment variable or pass it as an argument')
        print('Exiting...')
        sys.exit(1)

    apps_path = Path(apps_argument)
    if not apps_path.exists():
        print(f'"SCION_APPS_PATH={apps_path}", path does not exist!')
        if "~" in apps_argument:
            print('Expand the path (e.g. "~" -> "/home/[username]/")')
        print('Exiting...')
        sys.exit(1)

    if not apps_path.is_dir():
        print(f'"SCION_APPS_PATH={apps_path}", path is not a directory!')
        print('Exiting...')
        sys.exit(1)

    for app in ['bin/scion-bwtestclient', 'bin/scion-bwtestserver']:
        if not apps_path.joinpath(app).exists():
            print(f'"SCION_APPS_PATH={apps_path}", path does not contain "{app}"')
            print('Exiting...')
            sys.exit(1)

    return apps_path


def parse_arguments():
    parser = argparse.ArgumentParser(description='Starts all SCION topologies')
    parser.add_argument('-g', '--gen-path', default=GEN_PATH,
                        help='Path where you generated the output files in \
                         the build process')
    parser.add_argument('-a', '--apps', default=os.getenv('SCION_APPS_PATH'),
                        help='Path to SCION apps directory')

    args = parser.parse_args()

    apps_path = check_scion_apps(args.apps)

    return apps_path.absolute(), args.gen_path


def main():
    scion_apps_path, gen_path = parse_arguments()
    SCIONTopology(scion_apps_path, gen_path).start()


if __name__ == '__main__':
    main()
