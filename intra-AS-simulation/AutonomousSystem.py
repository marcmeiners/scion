#!/usr/bin/env python3
import os
import sys
import configparser
from collections import defaultdict
from pathlib import Path
from mininet.node import UserSwitch, Node
from mininet.net import Mininet
from mininet.link import TCULink
from routing_protocols import OSPF
from mininet.topo import Topo

sys.path.insert(0, '.')
from python.topology.supervisor import (
    SUPERVISOR_CONF,
)

class LinuxRouter( Node ):
    """Adapted from: mininet/examples/linuxrouter.py
    
    A Node with IP forwarding enabled.
    Reverse path (rp) filtering is disabled.
    Otherwise pinging in loop topologies is not fully functional.
    """

    # pylint: disable=arguments-differ
    def config( self, **params ):
        super( LinuxRouter, self).config( **params )
        # Enable forwarding on the router
        self.cmd( 'sysctl net.ipv4.ip_forward=1' )
        # Disable rp_filter on all interfaces -> allows/enables loop topologies
        self.cmd( 'for i in /proc/sys/net/ipv4/conf/*/rp_filter; do echo 0 > "$i" ; done' )

    def terminate( self ):
        self.cmd( 'sysctl net.ipv4.ip_forward=0' )
        self.cmd( 'for i in /proc/sys/net/ipv4/conf/*/rp_filter; do echo 1 > "$i" ; done' )
        super( LinuxRouter, self ).terminate()


class MininetWithControlNet( Mininet ):
    """Essentially the same as normal Mininet!
    
    Needed to use inNamespace option for the whole AS
    """

    def configureControlNetwork( self ):
        "Configure control network."
        pass

class Intra_AS_Topo( Topo ):
    """Class containing the topology of the AS."""
    
    def get_addresses(self, node_a, node_b, SUBNETS, config):
        """Helper function: Get the addresses for the links between the nodes."""
        a_addr, b_addr = None, None
        for subnet in SUBNETS:
            if subnet in self.subnet_taken:
                continue
            netmask = subnet.split('/')[-1]
            found1, found2 = False, False
            for name, ip in config[subnet].items():
                if name.split('@')[-1] == node_a:
                    a_addr = f'{ip}/{netmask}'
                    found1 = True
                if name.split('@')[-1] == node_b:
                    b_addr = f'{ip}/{netmask}'
                    found2 = True
            if found1 and found2:
                self.subnet_taken.append(subnet)
                break

        if not found1:
            raise Exception(f'{node_a} not found in networks config')
        if not found2:
            raise Exception(f'{node_b} not found in networks config')
        return a_addr, b_addr


    def build( self, networks_config, intra_topo_dict, SUBNETS ):
        """Build the topology.

        Adds the nodes and links to the topology as defined 
        in the intra topology file.
        Also adds link properties.
        """

        self.intra_links = defaultdict(lambda: defaultdict(dict))
        all_nodes = {}
        for _, node_list in intra_topo_dict['Nodes'].items():
            for node in node_list:
                all_nodes[node] = self.addHost(node, ip=None)
        
        self.link_nr = defaultdict(int)
        self.subnet_taken = []
        for link in intra_topo_dict['links']:
            node_a_name = link['a']
            node_b_name = link['b']
            node_a = all_nodes[node_a_name]
            node_b = all_nodes[node_b_name]

            a_addr, b_addr = self.get_addresses(node_a_name, node_b_name, SUBNETS, networks_config)

            # bw        bandwidth in Mbit/s (e.g. 10)
            # delay 	transmit delay (e.g. '1ms' )
            # jitter	jitter (e.g. '1ms')
            # loss	    loss (e.g. 2 ) 
            bw = link.get('bw', None)
            if bw is not None:
                bw = int(bw)
            delay = link.get('delay', None)
            jitter = link.get('jitter', None)
            loss = link.get('loss', None)                

            self.addLink(node_a, node_b,
                         bw=bw, delay=delay, jitter=jitter, loss=loss, 
                         params1={ 'ip' : a_addr}, 
                         params2={ 'ip' : b_addr} 
                         )

            # Adding MTU has to be done manually + after building Mininet
            if 'mtu' in link:
                nr = self.link_nr[(node_a_name, node_b_name)]
                self.intra_links[node_a][node_b][nr] = {
                                                'mtu' : link['mtu']
                                                }
            self.link_nr[(node_a_name, node_b_name)] += 1
            self.link_nr[(node_b_name, node_a_name)] += 1







class AutonomousSystem( object ):
    """Class containing the AS."""

    def __init__(self, SCION_topo, ISD_AS_id, FULL_NAME, intra_topology_dict, 
        routing_protocol, intra_topo_graph, **opts) -> None:
        for k, v in vars(SCION_topo).items():
            setattr(self, k, v)
        self.ISD_AS_id = ISD_AS_id
        self.FULL_NAME = FULL_NAME
        self.intra_topology_dict = intra_topology_dict
        self.default_routing_protocol = OSPF
        self.routing_protocol_name = routing_protocol
        self.use_supervisor = False
        self.cmd_prefix = f'sudo -u {self.username} bash -c '
        self.graph = intra_topo_graph
        


    def build(self):
        """Build the AS.
        
        Creates the topology and finally the Mininet network.
        Adds specified routing protocol.
        """
        self.gen_subnets()
        self.intra_topo = Intra_AS_Topo(
            networks_config=self.networks_config,
            intra_topo_dict=self.intra_topology_dict,
            SUBNETS=self.SUBNETS
            )
        self.net = MininetWithControlNet(
            topo=self.intra_topo,
            inNamespace=True, 
            switch=UserSwitch,
            link=TCULink,
            host=LinuxRouter,
            controller=None,
            waitConnected=True

        )
        self.add_addtional_link_config()
        self.start_intra_routing_protocol()


    def gen_subnets(self):
        """Helper function: Sets subnet attributes.
        
        If all addresses in a subnet contain the AS name, 
        then this subnet belongs to the AS.
        If only one address in a subnet contains the AS name, 
        then this subnet spans over two ASes, meaning subnet for border routers.
        """
        self.SUBNETS = []
        self.BR_SUBNETS = []
        for subnet in self.networks_config.sections():
            containing_name = [self.FULL_NAME in name for name in self.networks_config[subnet]]
            if all(containing_name):
                self.SUBNETS.append(subnet)
            elif any(containing_name):
                self.BR_SUBNETS.append(subnet)


    def add_addtional_link_config(self):
        """After Mininet is built, add additional link config.

        MTU for example can only be set after Mininet is built
        """
        for a, a_conf in self.intra_topo.intra_links.items():
            node_a = self.net.get(a)
            for b, b_conf in a_conf.items():
                node_b = self.net.get(b)
                for link_nr, conf in b_conf.items():
                    link = self.net.linksBetween(node_a, node_b)[link_nr]
                    intf1 = link.intf1.name
                    intf2 = link.intf2.name
                    if 'mtu' in conf:
                        mtu = conf['mtu']
                        node_a.cmd(f'ip link set dev {intf1} mtu {mtu}')
                        node_b.cmd(f'ip link set dev {intf2} mtu {mtu}')

        
    def start_intra_routing_protocol(self):
        self.protocol = self.default_routing_protocol(self)

        if self.routing_protocol_name == 'OSPF':
            self.protocol = OSPF(self)

        self.protocol.start()



 
    def create_disp_config_file(self, name):
        """Create dispatcher file for the node with the given name.
        
        Important part is: It sets the socket file explicitly.
        """
        socket_file = Path(f"/run/shm/dispatcher/disp_{self.FULL_NAME}_{name}.sock")
        # remove socket file - if it exists - because caused problems -> "address already in use"
        if socket_file.exists():
            socket_file.unlink()

        config = f"""[dispatcher]
                id = "dispatcher"
                application_socket = "{socket_file}"

                [metrics]
                prometheus = "[127.0.0.1]:30441"

                [features]

                [api]
                addr = "[127.0.0.1]:31141"

                [log.console]
                level = "debug"
                """
        config_file = Path(self.gen_dir, f'dispatcher/disp_{self.FULL_NAME}_{name}.toml',)
        with open(config_file, mode='w') as f:
            f.writelines(config)
        return config_file, socket_file


    def start_dispatcher(self, node):
        """Start dispatcher for the given node."""
        name = node.name
        config_file, socket_file = self.create_disp_config_file(name)
        node.cmd( f'{self.cmd_prefix} "cd {self.SCION_PATH} ; bin/dispatcher --config {config_file} >logs/dispatcher_{self.FULL_NAME}_{name}.log 2>&1 &"')
        pid = node.cmd( f'pgrep -f "dispatcher --config {config_file}"').strip()
        return pid, socket_file


    def change_config(self, command, socket_file):
        """Changes Colibri + Control Service configuration.
        
        Specifies/Changes the socket file for the dispatcher.
        """
        config_file = command.split('--config')[1].strip()
        config_file = Path(self.SCION_PATH, config_file)
        config = configparser.RawConfigParser()
        config.read(config_file)
        config.set("general", "dispatcher_socket", f'"{socket_file}"')

        with open(config_file, mode='w') as f:
            config.write(f)

    def add_SCION_services(self, BR_name_map):
        """Add + start SCION services to the correct node."""
        SUPERVISORD_FILE = Path(self.gen_dir, SUPERVISOR_CONF)
        supervisor_config = configparser.ConfigParser()
        supervisor_config.read(SUPERVISORD_FILE)

        nodes = self.intra_topology_dict['Nodes']


        self.started_pids = []
        nr_services = defaultdict(int)
        for node in self.net.hosts:
            name = node.name
            if name in nodes['Internal-Router']:
                # no SCION service to start here
                continue 
            if name not in nodes['Borderrouter']:
                disp_pid, socket_file = self.start_dispatcher(node)
                self.started_pids.append(disp_pid)


            if name in nodes['Colibri']:
                # print('### Starting colibri ###\n')
                nr_services['Colibri'] += 1
                nr = nr_services['Colibri']
                command = supervisor_config[f'program:co{self.FULL_NAME}-{nr}-@{name}']['command']
                self.change_config(command, socket_file)
                logfile = supervisor_config[f'program:co{self.FULL_NAME}-{nr}-@{name}']['stdout_logfile']
                service = f'*co{self.FULL_NAME}-{nr}*'

            
            elif name in nodes['Control-Service']:
                # print('### Starting control service ###\n')
                nr_services['Control-Service'] += 1
                nr = nr_services['Control-Service']
                command = supervisor_config[f'program:cs{self.FULL_NAME}-{nr}-@{name}']['command']
                self.change_config(command, socket_file)
                logfile = supervisor_config[f'program:cs{self.FULL_NAME}-{nr}-@{name}']['stdout_logfile']
                service = f'*cs{self.FULL_NAME}-{nr}*'


            elif name in nodes['SCION-Daemon']:
                # print('### Starting SCION daemon ###\n')
                nr_services['Control-Service'] += 1
                command = supervisor_config[f'program:sd{self.FULL_NAME}']['command']
                logfile = supervisor_config[f'program:sd{self.FULL_NAME}']['stdout_logfile']
                service = f'*sd{self.FULL_NAME}'


            elif name in nodes['Borderrouter']:
                # print('### Starting border router ###\n')
                BR_name = BR_name_map[name]
                command = supervisor_config[f'program:{BR_name}']['command']
                logfile = supervisor_config[f'program:{BR_name}']['stdout_logfile']
                service = f'*{BR_name}*'

            else:
                # this is a normal client, starting dispatcher is enough
                continue


        
            if self.use_supervisor: 
                # DOES NOT WORK YET
                # feel free to fix it
                final_command = f'{self.cmd_prefix} "cd {self.SCION_PATH} ; export PATH=\'~/.local/bin:$PATH\' ;  supervisor/supervisor.sh mstart {service} "'
                out = node.cmd(final_command)
                print(f'Output supervisor->{out}')
            else: 
                node.cmd(f'{self.cmd_prefix} "cd {self.SCION_PATH} ; {command} >{logfile} 2>&1 &" ')
                pid = node.cmd(f'pgrep -f "{command}"').strip()
                self.started_pids.append(pid)


    def start(self):
        """Start Mininet network."""
        self.net.start()

    def stop(self):
        """Stop all processes."""
        for pid in self.started_pids:
            os.system(f'kill -9 {pid}')
        self.protocol.stop()
        self.net.stop()