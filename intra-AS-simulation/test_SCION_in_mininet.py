#!/usr/bin/env python3
import json
from pathlib import Path
from mininet.log import lg, info, output, error


def get_sciond_addresses(AS):
    """Reads the sciond addresses from the sciond_addresses.json file"""
    SCIOND_FILE = Path(AS.gen_dir, "sciond_addresses.json")

    with open(SCIOND_FILE) as f:
        sciond_addresses = json.load(f)
    return sciond_addresses


def get_destinations(ASes, AS):
    """Returns a list of all SCION destinations in the topology

    This list does not include the border routers.
    """
    ALL_ASs = []
    for _, value_list in AS.AS_yaml_dict.items():
        ALL_ASs += value_list

    all_destinations = []
    for subnet in AS.networks_config.sections():
        for AS_NAME in ALL_ASs:
            new_AS_NAME = AS_NAME.replace(":", "_")
            for name, ip in AS.networks_config[subnet].items():
                if new_AS_NAME in name and '@' in name:
                    internal_name = name.split('@')[1]
                    nodes = ASes[AS_NAME]['intra_topology_dict']['Nodes']
                    if (internal_name in nodes['Borderrouter']
                            or internal_name in nodes['Internal-Router']):
                        continue
                    all_destinations.append(f'{AS_NAME},{ip}')

    return all_destinations


def test_SCION_ping(ASes, AS):
    """Test SCION ping

    Ping from each SCION node in this AS to each other SCION node in the whole topology
    Except for the border routers!
    """
    info('---starting SCION ping---\n')
    ISD_AS_id = AS.ISD_AS_id
    net = AS.net
    all_destinations = get_destinations(ASes, AS)
    sciond_addresses = get_sciond_addresses(AS)
    nodes = ASes[ISD_AS_id]['intra_topology_dict']['Nodes']

    # Get sciond address of own AS
    ip_sciond = None
    for name, addr in sciond_addresses.items():
        if name.startswith(ISD_AS_id):
            ip_sciond = addr
    if ip_sciond is None:
        raise Exception(f'No sciond address found for AS {ISD_AS_id}')

    # Loop through all SCION nodes in own AS
    for host in net.hosts:
        name = host.name
        if name in nodes['Borderrouter'] or name in nodes['Internal-Router']:
            continue
        output(f'{name} -> \n')
        # Get SCION dispatcher socket of specific SCION node in own AS
        dispatcher_socket = f"/run/shm/dispatcher/disp_{AS.FULL_NAME}_{name}.sock"
        # dispatcher_socket = f'/run/shm/dispatcher/default.sock'
        # all_destination is array of tuples AS_NAME,ip
        for destination in all_destinations:
            output(f'\t{destination} -> ')
            # --sciond specifies SCION Daemon address for the given AS
            # --dispatcher is the specific SCION dispatcher for the node
            command = f"cd {AS.SCION_PATH} ; \
                        ./bin/scion ping --sciond {ip_sciond}:30255 \
                        --dispatcher {dispatcher_socket} {destination} -c 1 --timeout 3s"
            result = host.cmd(command)
            sent, received = net._parsePing(result)
            if received > sent:
                error('*** Error: received too many packets')
                error(f'{result}')
                # exit(1)
            if received:
                output('OK\n')
            else:
                output('FAILED\n')

        output('\n')
    info('---finished SCION ping---\n')


def get_sciond_map(ASes, AS):
    """Returns a map of ASes to their sciond addresses"""
    sciond_addresses = get_sciond_addresses(AS)
    AS_daemon_map = {}
    for ASstr in ASes.keys():
        for name, addr in sciond_addresses.items():
            if name.startswith(ASstr):
                AS_daemon_map[ASstr] = addr
                break
    return AS_daemon_map


def get_client_map(ASes, networks):
    """Returns a map of ASes to their client addresses"""
    AS_client_map = {}
    for ASstr in ASes.keys():
        nodes = ASes[ASstr]['intra_topology_dict']['Nodes']
        AS_net = networks[ASstr]['AS'].net
        for node in AS_net.hosts:
            if node.name in nodes['Client']:
                AS_client_map[ASstr] = node
                break
    return AS_client_map


def test_SCION_bwtest_client_server(ASes, networks, AS):
    """Bandwidth test from this AS to all other ASes.

    Asks user to enter bandwidth to test.
    Starts the Bandwidth server in the given AS.
    Then starts the Bandwidth client in all the other ASes.
    """
    ISD_AS_id = AS.ISD_AS_id
    SCION_APPS = AS.SCION_APPS_PATH
    PORT = 12345

    bw = input('Enter bandwidth in Mbps [Press Enter for default=1Mbps]: ').strip(
    ).replace(' ', '')
    if not bw:
        output('Using default bandwidth of 1Mbps\n')
        bw = '1'
    CS_string = f'?,1000,?,{bw}Mbps'
    SC_string = f'?,1000,?,{bw}Mbps'

    AS_daemon_map = get_sciond_map(ASes, AS)
    server_ip_sciond = AS_daemon_map.get(ISD_AS_id, None)
    if server_ip_sciond is None:
        raise Exception(f'No sciond address found for AS {ISD_AS_id}')

    AS_client_map = get_client_map(ASes, networks)
    server_node = AS_client_map.get(ISD_AS_id, None)
    if server_node is None:
        raise Exception(f'No client found in {ISD_AS_id}, can not run BW test')

    # start bw server
    info('---starting SCION bwtest server ---\n')
    server_name = server_node.name
    sever_dispatcher_socket = f"/run/shm/dispatcher/disp_{AS.FULL_NAME}_{server_name}.sock"
    command = f'cd {SCION_APPS} ; \
                export SCION_DAEMON_ADDRESS="{server_ip_sciond}:30255" ; \
                export SCION_DISPATCHER_SOCKET={sever_dispatcher_socket} ; \
                ./bin/scion-bwtestserver --listen=:{PORT} &'
    lg.debug(command)
    if server_node.waiting:
        # if user terminates previous run with CTRL-C:
        # give time for the process to stop
        # remove produced output + reset node to waiting=False
        server_node.monitor(timeoutms=2000)
        server_node.waiting = False
    server_node.cmd(command)
    server_addr = f'{ISD_AS_id},{server_node.IP()}'

    for ASstr in ASes.keys():
        if ASstr == ISD_AS_id:
            # in AS we run bwtest server
            continue
        output(f'##### Starting bwtest client for AS={ASstr}            #####\n')
        output(f'##### Execute bandwidth test with server in AS={ISD_AS_id} #####\n')
        client_node = AS_client_map[ASstr]
        client_name = client_node.name
        client_daemon = AS_daemon_map[ASstr]
        client_FULL_NAME = ASes[ASstr]['FULL_NAME']
        client_dispatcher_socket = f"/run/shm/dispatcher/disp_{client_FULL_NAME}_{client_name}.sock"
        command = f'cd {SCION_APPS} ; \
                export SCION_DAEMON_ADDRESS="{client_daemon}:30255" ; \
                export SCION_DISPATCHER_SOCKET={client_dispatcher_socket} ; \
                ./bin/scion-bwtestclient -s {server_addr}:{PORT} -sc {SC_string} -cs {CS_string}'
        lg.debug(command)
        if client_node.waiting:
            client_node.monitor(timeoutms=2000)
            client_node.waiting = False
        result = client_node.cmd(command)
        output(f'Bandwidth test result:\n{result}')
        output('\n')

    # terminating bw server
    info('---Terminating bwtest server ---\n')
    result = server_node.cmd('killall scion-bwtestserver')
    info('---Finished SCION bwtest---\n')
