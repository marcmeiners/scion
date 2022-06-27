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
:mod:`topo` --- SCION topology topo generator
=============================================
"""
# Stdlib
import json
import logging
import os
import random
import sys
from collections import defaultdict

# External packages
import yaml

# SCION
from python.lib.defines import (
    AS_LIST_FILE,
    IFIDS_FILE,
    BR_NAMES_FILE,
    SCION_MIN_MTU,
    SCION_ROUTER_PORT,
    TOPO_FILE,
)
from python.lib.types import LinkType
from python.lib.util import write_file
from python.topology.common import (
    ArgsBase,
    join_host_port,
    json_default,
    SCION_SERVICE_NAMES,
    TopoID
)
from python.topology.net import (
    PortGenerator,
    SubnetGenerator
)

DEFAULT_BEACON_SERVERS = 1
DEFAULT_CONTROL_SERVERS = 1
DEFAULT_COLIBRI_SERVERS = 1

UNDERLAY_4 = 'UDP/IPv4'
UNDERLAY_6 = 'UDP/IPv6'
DEFAULT_UNDERLAY = UNDERLAY_4
ADDR_TYPE_4 = 'IPv4'
ADDR_TYPE_6 = 'IPv6'


class TopoGenArgs(ArgsBase):
    def __init__(self,
                 args: ArgsBase,
                 topo_config,
                 intra_config,
                 intra_topo_dicts: dict,
                 subnet_gen4: SubnetGenerator,
                 subnet_gen6: SubnetGenerator,
                 default_mtu: int):
        """
        :param ArgsBase args: Contains the passed command line arguments.
        :param dict topo_config: The parsed topology config.
        :param SubnetGenerator subnet_gen4: The default network generator for IPv4.
        :param SubnetGenerator subnet_gen6: The default network generator for IPv6.
        :param dict default_mtu: The default mtu.
        """
        super().__init__(args)
        self.topo_config_dict = topo_config
        self.intra_config_dict = intra_config
        self.intra_topo_dicts = intra_topo_dicts
        self.subnet_gen = {
            ADDR_TYPE_4: subnet_gen4,
            ADDR_TYPE_6: subnet_gen6,
        }
        self.default_mtu = default_mtu
        self.port_gen = PortGenerator()


class TopoGenerator(object):
    def __init__(self, args):
        """
        :param TopoGenArgs args: Contains the passed command line arguments.
        """
        self.args = args
        self.topo_dicts = {}
        self.hosts = []
        self.virt_addrs = set()
        self.as_list = defaultdict(list)
        self.links = defaultdict(list)
        self.ifid_map = {}
        self.BR_orig_str_map = {}
        self.BR_names = {}

    def _reg_addr(self, topo_id: TopoID, elem_id, addr_type):
        subnet = self.args.subnet_gen[addr_type].register(str(topo_id))
        if self.args.docker and addr_type == ADDR_TYPE_6:
            # for docker also allocate an IPv4 address so that we have ipv4
            # range allocated for the network.
            v4subnet = self.args.subnet_gen[ADDR_TYPE_4].register(str(topo_id) + '_v4')
            v4subnet.register(elem_id + '_v4')
        return subnet.register(elem_id)

    def _reg_link_addrs(self, local_br, remote_br, local_ifid, remote_ifid, addr_type):
        link_name = str(sorted((local_br, remote_br)))
        link_name += str(sorted((local_ifid, remote_ifid)))
        subnet = self.args.subnet_gen[addr_type].register(link_name)
        if self.args.docker and addr_type == ADDR_TYPE_6:
            # for docker also allocate an IPv4 address so that we have ipv4
            # range allocated for the network.
            v4subnet = self.args.subnet_gen[ADDR_TYPE_4].register(link_name + '_v4')
            v4subnet.register(local_br + '_v4')
        return subnet.register(local_br), subnet.register(remote_br)

    def _iterate(self, f):
        for isd_as, as_conf in self.args.topo_config_dict["ASes"].items():
            f(TopoID(isd_as), as_conf)

    def generate(self):
        self._read_links()
        # in a first step we allocate all networks, so that we can later use
        # the IPs in the generate functions.
        if self.args.intra_config is not None: 
            self._iterate(self._register_intra_addrs)
        else: 
            self._iterate(self._register_addrs)
        networks = {}
        for k, v in self.args.subnet_gen[ADDR_TYPE_4].alloc_subnets().items():
            networks[k] = v
        for k, v in self.args.subnet_gen[ADDR_TYPE_6].alloc_subnets().items():
            networks[k] = v
        self._iterate(self._generate_as_topo)
        self._iterate(self._generate_as_list)
        self._iterate(self._write_as_topo)
        self._write_as_list()
        self._write_ifids()
        self._write_BR_name_mapping()
        return self.topo_dicts, networks

    def get_nick_intra(self, node, intra_nodes_dict):
        if node in intra_nodes_dict['Colibri']:
            return 'co'
        if node in intra_nodes_dict['Control-Service']:
            return 'cs'
        if node in intra_nodes_dict['SCION-Daemon']:
            return 'sd'
        if node in intra_nodes_dict['Borderrouter']:
            return 'br'
        if node in intra_nodes_dict['Client']:
            return 'cl'
        return 'r'

    def _register_intra_addrs(self, topo_id, as_conf):
        intra_topo_file = self.args.intra_topo_dicts[topo_id.__str__()]
        intra_nodes_dict = intra_topo_file['Nodes']
        intra_links = intra_topo_file['links']
        addr_type = addr_type_from_underlay(as_conf.get('underlay', DEFAULT_UNDERLAY))

        seen = defaultdict(int)

        for i, link in enumerate(intra_links):
            a = link['a']
            nick_a = self.get_nick_intra(a, intra_nodes_dict)
            b = link['b']
            nick_b = self.get_nick_intra(b, intra_nodes_dict)
            seen[a] += 1
            seen[b] += 1
            a_id = "%s%s-%s-@%s" % (nick_a, topo_id.file_fmt(), seen[a], a)
            b_id = "%s%s-%s-@%s" % (nick_b, topo_id.file_fmt(), seen[b], b)



            self._reg_link_addrs(a_id, b_id, seen[a], seen[b], addr_type)
            if not self.args.docker:
                self.args.port_gen.register(a_id)
                self.args.port_gen.register(b_id)

        for (linkto, remote, attrs, l_br, r_br, l_ifid, r_ifid) in self.links[topo_id]:
            link_addr_type = addr_type_from_underlay(attrs.get('underlay', DEFAULT_UNDERLAY))
            self._reg_link_addrs(l_br, r_br, l_ifid, r_ifid, link_addr_type)


    def _register_addrs(self, topo_id, as_conf):
        self._register_srv_entries(topo_id, as_conf)
        self._register_br_entries(topo_id, as_conf)
        if self.args.sig:
            self._register_sig(topo_id, as_conf)
        self._register_sciond(topo_id, as_conf)

    def _register_srv_entries(self, topo_id, as_conf):
        srvs = [("control_servers", DEFAULT_CONTROL_SERVERS, "cs")]
        srvs.append(("colibri_servers", DEFAULT_COLIBRI_SERVERS, "co"))
        for conf_key, def_num, nick in srvs:
            self._register_srv_entry(topo_id, as_conf, conf_key, def_num, nick)

    def _register_srv_entry(self, topo_id, as_conf, conf_key, def_num, nick):
        addr_type = addr_type_from_underlay(as_conf.get('underlay', DEFAULT_UNDERLAY))
        count = self._srv_count(as_conf, conf_key, def_num)
        for i in range(1, count + 1):
            elem_id = "%s%s-%s" % (nick, topo_id.file_fmt(), i)
            if not self.args.docker:
                self.args.port_gen.register(elem_id)
            self._reg_addr(topo_id, elem_id, addr_type)

    def _register_br_entries(self, topo_id, as_conf):
        addr_type = addr_type_from_underlay(as_conf.get('underlay', DEFAULT_UNDERLAY))
        for (linkto, remote, attrs, l_br, r_br, l_ifid, r_ifid) in self.links[topo_id]:
            self._register_br_entry(topo_id, l_ifid, remote, r_ifid,
                                    linkto, attrs, l_br, r_br, addr_type)

    def _register_br_entry(self, local, l_ifid, remote, r_ifid, remote_type, attrs,
                           local_br, remote_br, addr_type):
        link_addr_type = addr_type_from_underlay(attrs.get('underlay', DEFAULT_UNDERLAY))
        self._reg_link_addrs(local_br, remote_br, l_ifid, r_ifid, link_addr_type)
        self._reg_addr(local, local_br + "_internal", addr_type)
        if not self.args.docker:
            self.args.port_gen.register(local_br + "_internal")

    def _register_sig(self, topo_id, as_conf):
        addr_type = addr_type_from_underlay(as_conf.get('underlay', DEFAULT_UNDERLAY))
        self._reg_addr(topo_id, "sig" + topo_id.file_fmt(), addr_type)

    def _register_sciond(self, topo_id, as_conf):
        addr_type = addr_type_from_underlay(as_conf.get('underlay', DEFAULT_UNDERLAY))
        self._reg_addr(topo_id, "sd" + topo_id.file_fmt(), addr_type)
        # Always register the tester element. This causes the generator to create a
        # bridge in the docker topology, which SCIOND, SIG (if enabled) and
        # client applications can use to communicate.
        self._reg_addr(topo_id, "tester_" + topo_id.file_fmt(), addr_type)

    def _br_name(self, ep, assigned_br_id, br_ids, if_ids):
        br_name = ep.br_name()
        if br_name:
            # BR with multiple interfaces, reuse assigned id
            br_id = assigned_br_id.get(br_name)
            if br_id is None:
                # assign new id
                br_ids[ep] += 1
                assigned_br_id[br_name] = br_id = br_ids[ep]
        else:
            # BR with single interface
            br_ids[ep] += 1
            br_id = br_ids[ep]
        br = "br%s-%d" % (ep.file_fmt(), br_id)
        ifid = ep.ifid
        if self.args.random_ifids or not ifid:
            ifid = if_ids[ep].new()
        else:
            if_ids[ep].add(ifid)
        return br, ifid

    def _get_orig_str(self, x):
        x_no_itf = x.split('#')[0]
        x_split = x_no_itf.split('-')
        if len(x_split) == 3: 
            # specific ID is given
            # don't save interface, because this BR will have multiple interfaces
            return x_no_itf
        # no specific ID is given, all interfaces important
        return x     

    def _read_links(self):
        assigned_br_id = {}
        br_ids = defaultdict(int)
        if_ids = defaultdict(lambda: IFIDGenerator())
        if not self.args.topo_config_dict.get("links", None):
            return
        for attrs in self.args.topo_config_dict["links"]:
            initial_a = attrs['a']
            initial_b = attrs['b']
            orig_a = self._get_orig_str(initial_a)
            orig_b = self._get_orig_str(initial_b)
            a = LinkEP(attrs.pop("a"))
            b = LinkEP(attrs.pop("b"))
            linkto = linkto_a = linkto_b = attrs.pop("linkAtoB")
            if linkto.lower() == LinkType.CHILD:
                linkto_a = LinkType.PARENT
                linkto_b = LinkType.CHILD
            a_br, a_ifid = self._br_name(a, assigned_br_id, br_ids, if_ids)
            b_br, b_ifid = self._br_name(b, assigned_br_id, br_ids, if_ids)
            self.links[a].append((linkto_b, b, attrs, a_br, b_br, a_ifid, b_ifid))
            self.links[b].append((linkto_a, a, attrs, b_br, a_br, b_ifid, a_ifid))
            a_desc = "%s %s" % (a_br, a_ifid)
            b_desc = "%s %s" % (b_br, b_ifid)
            self.ifid_map.setdefault(str(a), {})
            self.ifid_map[str(a)][a_desc] = b_desc
            self.ifid_map.setdefault(str(b), {})
            self.ifid_map[str(b)][b_desc] = a_desc
            self.BR_names.setdefault(str(a), {})
            self.BR_names[str(a)][initial_a] = a_br
            self.BR_names.setdefault(str(b), {})
            self.BR_names[str(b)][initial_b] = b_br
            self.BR_orig_str_map[(a_br,a_ifid)] = orig_a
            self.BR_orig_str_map[(b_br,b_ifid)] = orig_b

    def _generate_as_topo(self, topo_id, as_conf):
        mtu = as_conf.get('mtu', self.args.default_mtu)
        assert mtu >= SCION_MIN_MTU, mtu
        attributes = []
        for attr in ['authoritative', 'core', 'issuing', 'voting']:
            if as_conf.get(attr, False):
                attributes.append(attr)
        self.topo_dicts[topo_id] = {
            'attributes': attributes,
            'isd_as': str(topo_id),
            'mtu': mtu,
        }
        for i in SCION_SERVICE_NAMES:
            self.topo_dicts[topo_id][i] = {}

        if self.args.intra_config is not None:
            self._gen_intra_entries(topo_id, as_conf)
        else: 
            self._gen_srv_entries(topo_id, as_conf)
            self._gen_br_entries(topo_id, as_conf)
            if self.args.sig:
                self.topo_dicts[topo_id]['sigs'] = {}
                self._gen_sig_entries(topo_id, as_conf)

    def _gen_intra_srvs(self, port, elem_id, topo_id, topo_key, ip):
        if not self.args.docker:
            port = self.args.port_gen.register(elem_id)

        d = {
            'addr': join_host_port(ip, port),
        }
        self.topo_dicts[topo_id][topo_key][elem_id] = d

    def _gen_intra_entries(self, topo_id, as_conf):
        intra_topo_file = self.args.intra_topo_dicts[topo_id.__str__()]
        borderrouter_dict = self.args.intra_config_dict['ASes'][topo_id.__str__()]['Borderrouter']
        intra_nodes_dict = intra_topo_file['Nodes']
        intra_links = intra_topo_file['links']
        addr_type = addr_type_from_underlay(as_conf.get('underlay', DEFAULT_UNDERLAY))

        seen = defaultdict(int)
        borderrouters = {}

        for i, link in enumerate(intra_links):
            a = link['a']
            nick_a = self.get_nick_intra(a, intra_nodes_dict)
            b = link['b']
            nick_b = self.get_nick_intra(b, intra_nodes_dict)
            seen[a] += 1
            seen[b] += 1
            a_id = "%s%s-%s-@%s" % (nick_a, topo_id.file_fmt(), seen[a], a)
            b_id = "%s%s-%s-@%s" % (nick_b, topo_id.file_fmt(), seen[b], b)

            ip = self._reg_link_addrs(a_id, b_id, seen[a], seen[b], addr_type)[0].ip

            for node, ID in [(a, a_id), (b, b_id)]:
                if node in intra_nodes_dict['Colibri']:
                    port = self._default_ctrl_port('co')
                    self._gen_intra_srvs(port, ID, topo_id, "colibri_service", ip)

                if node in intra_nodes_dict['Control-Service']:
                    port = self._default_ctrl_port('cs')
                    self._gen_intra_srvs(port, ID, topo_id, "control_service", ip)
                    self._gen_intra_srvs(port, ID, topo_id, "discovery_service", ip)

            if link['a'] in intra_nodes_dict['Borderrouter']:
                borderrouters[link['a']] = {'a_id': a_id, 'b_id': b_id, 'seen_a' : seen[a], 'seen_b' : seen[b]}
            if link['b'] in intra_nodes_dict['Borderrouter']:
                borderrouters[link['b']] = {'a_id': b_id, 'b_id': a_id, 'seen_a' : seen[b], 'seen_b' : seen[a]}
    
        for (remote_type, remote, attrs, l_br, r_br, l_ifid, r_ifid) in self.links[topo_id]:
            link_addr_type = addr_type_from_underlay(attrs.get('underlay', DEFAULT_UNDERLAY))
            public_addr, remote_addr = self._reg_link_addrs(l_br, r_br, l_ifid,
                                                            r_ifid, link_addr_type)


            local_br = self.BR_orig_str_map[(l_br, l_ifid)]
            for internal_name, br_name in borderrouter_dict.items():
                if local_br == br_name:
                    internal_br = internal_name
                    break

            a_id = borderrouters[internal_br]['a_id']
            b_id = borderrouters[internal_br]['b_id']
            seen_a = borderrouters[internal_br]['seen_a']
            seen_b = borderrouters[internal_br]['seen_b']


            intl_addr = self._reg_link_addrs(a_id, b_id, seen_a, seen_b, addr_type)[0]
            if self.topo_dicts[topo_id]["border_routers"].get(l_br) is None:
                intl_port = 30042
                if not self.args.docker:
                    intl_port = self.args.port_gen.register(a_id)

                self.topo_dicts[topo_id]["border_routers"][l_br] = {
                    'internal_addr': join_host_port(intl_addr.ip, intl_port),
                    'interfaces': {
                        l_ifid: self._gen_br_intf(remote, public_addr, remote_addr, attrs, remote_type)
                    }
                }
            else:
                # There is already a BR entry, add interface
                intf = self._gen_br_intf(remote, public_addr, remote_addr, attrs, remote_type)
                self.topo_dicts[topo_id]["border_routers"][l_br]['interfaces'][l_ifid] = intf



    def _gen_srv_entries(self, topo_id, as_conf):
        srvs = [("control_servers", DEFAULT_CONTROL_SERVERS, "cs", "control_service")]
        srvs.append(("control_servers", DEFAULT_CONTROL_SERVERS, "cs", "discovery_service"))
        srvs.append(("colibri_servers", DEFAULT_COLIBRI_SERVERS, "co", "colibri_service"))
        for conf_key, def_num, nick, topo_key in srvs:
            self._gen_srv_entry(topo_id, as_conf, conf_key, def_num, nick, topo_key)

    def _gen_srv_entry(self, topo_id, as_conf, conf_key, def_num, nick,
                       topo_key, uses_dispatcher=True):
        addr_type = addr_type_from_underlay(as_conf.get('underlay', DEFAULT_UNDERLAY))
        count = self._srv_count(as_conf, conf_key, def_num)
        for i in range(1, count + 1):
            elem_id = "%s%s-%s" % (nick, topo_id.file_fmt(), i)

            port = self._default_ctrl_port(nick)
            if not self.args.docker:
                port = self.args.port_gen.register(elem_id)

            d = {
                'addr': join_host_port(self._reg_addr(topo_id, elem_id, addr_type).ip, port),
            }
            self.topo_dicts[topo_id][topo_key][elem_id] = d

    def _default_ctrl_port(self, nick):
        if nick == "cs":
            return 30252
        if nick == "co":
            return 30257
        print('Invalid nick: %s' % nick)
        sys.exit(1)

    def _srv_count(self, as_conf, conf_key, def_num):
        count = as_conf.get(conf_key, def_num)
        if conf_key == "control_servers":
            count = 1
        return count

    def _gen_br_entries(self, topo_id, as_conf):
        addr_type = addr_type_from_underlay(as_conf.get('underlay', DEFAULT_UNDERLAY))
        for (linkto, remote, attrs, l_br, r_br, l_ifid, r_ifid) in self.links[topo_id]:
            self._gen_br_entry(topo_id, l_ifid, remote, r_ifid,
                               linkto, attrs, l_br, r_br, addr_type)

    def _gen_br_entry(self, local, l_ifid, remote, r_ifid, remote_type, attrs,
                      local_br, remote_br, addr_type):
        link_addr_type = addr_type_from_underlay(attrs.get('underlay', DEFAULT_UNDERLAY))
        public_addr, remote_addr = self._reg_link_addrs(local_br, remote_br, l_ifid,
                                                        r_ifid, link_addr_type)

        intl_addr = self._reg_addr(local, local_br + "_internal", addr_type)
        if self.topo_dicts[local]["border_routers"].get(local_br) is None:
            intl_port = 30042
            if not self.args.docker:
                intl_port = self.args.port_gen.register(local_br + "_internal")

            self.topo_dicts[local]["border_routers"][local_br] = {
                'internal_addr': join_host_port(intl_addr.ip, intl_port),
                'interfaces': {
                    l_ifid: self._gen_br_intf(remote, public_addr, remote_addr, attrs, remote_type)
                }
            }
        else:
            # There is already a BR entry, add interface
            intf = self._gen_br_intf(remote, public_addr, remote_addr, attrs, remote_type)
            self.topo_dicts[local]["border_routers"][local_br]['interfaces'][l_ifid] = intf

    def _fill_dict(self, dict, attrs, attribute):
        if attrs.get(attribute, None) is not None:
            dict[attribute] = attrs[attribute]
        return dict

    def _gen_br_intf(self, remote, public_addr, remote_addr, attrs, remote_type):
        optional_attrs = {}
        optional_attrs = self._fill_dict(optional_attrs, attrs, 'bw')
        optional_attrs = self._fill_dict(optional_attrs, attrs, 'delay')
    
        return {
            'underlay': {
                'public': join_host_port(public_addr.ip, SCION_ROUTER_PORT),
                'remote': join_host_port(remote_addr.ip, SCION_ROUTER_PORT),
            },
            'isd_as': str(remote),
            'link_to': LinkType.to_str(remote_type.lower()),
            'mtu': attrs.get('mtu', self.args.default_mtu),
            **optional_attrs
        }

    def _gen_sig_entries(self, topo_id, as_conf):
        addr_type = addr_type_from_underlay(DEFAULT_UNDERLAY)
        elem_id = "sig" + topo_id.file_fmt()
        reg_id = "sig" + topo_id.file_fmt()
        port = 30256
        if not self.args.docker:
            port = self.args.port_gen.register(elem_id)
        d = {
            'ctrl_addr': join_host_port(self._reg_addr(topo_id, reg_id, addr_type).ip, port),
            'data_addr': join_host_port(self._reg_addr(topo_id, reg_id, addr_type).ip, 30056),
        }
        self.topo_dicts[topo_id]['sigs'][elem_id] = d

    def _generate_as_list(self, topo_id, as_conf):
        if as_conf.get('core', False):
            key = "Core"
        else:
            key = "Non-core"
        self.as_list[key].append(str(topo_id))

    def _write_as_topo(self, topo_id, _as_conf):
        path = os.path.join(topo_id.base_dir(self.args.output_dir), TOPO_FILE)
        contents_json = json.dumps(self.topo_dicts[topo_id],
                                   default=json_default, indent=2)
        write_file(path, contents_json + '\n')

    def _write_as_list(self):
        list_path = os.path.join(self.args.output_dir, AS_LIST_FILE)
        write_file(list_path, yaml.dump(dict(self.as_list)))

    def _write_ifids(self):
        list_path = os.path.join(self.args.output_dir, IFIDS_FILE)
        write_file(list_path, yaml.dump(self.ifid_map,
                                        default_flow_style=False))

    def _write_BR_name_mapping(self):
        list_path = os.path.join(self.args.output_dir, BR_NAMES_FILE)
        write_file(list_path, yaml.dump(self.BR_names,
                                        default_flow_style=False))

class LinkEP(TopoID):
    def __init__(self, raw):
        self._brid = None
        self.ifid = None
        isd_as = raw
        parts = raw.split('#')
        if len(parts) == 2:
            self.ifid = int(parts[1])
            isd_as = parts[0]
        parts = isd_as.split("-")
        if len(parts) == 3:
            self._brid = parts[2]
            isd_as = "%s-%s" % (parts[0], parts[1])
        super().__init__(isd_as)

    def br_name(self):
        if self._brid is not None:
            return "%s-%s" % (self.file_fmt(), self._brid)
        return None


class IFIDGenerator(object):
    """Generates unique interface IDs"""

    def __init__(self):
        self._ifids = set()

    def new(self):
        while True:
            ifid = random.randrange(1, 4096)
            if ifid in self._ifids:
                continue
            self.add(ifid)
            return ifid

    def add(self, ifid):
        if ifid in self._ifids:
            logging.critical("IFID %d already exists!" % ifid)
            exit(1)
        if ifid < 1 or ifid > 4095:
            logging.critical("IFID %d is invalid!" % ifid)
            exit(1)
        self._ifids.add(ifid)


def addr_type_from_underlay(underlay: str) -> str:
    return underlay.split('/')[1]
