from mininet.node import UserSwitch, Node
from mininet.net import Mininet
from mininet.link import TCULink

class IntraColibri(object):
    def __init__(self, AS):
        self.FULL_NAME = AS.FULL_NAME
        self.net = AS.net
        self.SCION_PATH = AS.SCION_PATH
        self.nodes = self.net.hosts

    # TODO: Remove this function after manually adding qdiscs in start_simulation
    def startColibri(self):
        for node in self.nodes:
            name = node.name
            #This returns all interfaces of a node - in case of a border router only internal interfaces
            interfaces = node.intfNames()
            for intf in interfaces:
                #add priority and default queue as subclasses to existing Mininet qdisc configuration whitch holds link properties
                    #node.cmd(f"sudo tc class add dev {intf} parent 1:1 classid 1:10 prio 1")
                    #node.cmd(f"sudo tc class add dev {intf} parent 1:1 classid 1:20 prio 2")
                #if a packet has Linux Priority 2 (TOS 0x20), i.e. is a colibri packet, add it to the queue with higher prio
                    #node.cmd(f"sudo tc filter add dev {intf} parent 1: basic match 'meta(priority eq 2)' classid 1:10")
                    
                #currently it is sufficient to use classless qdisc with 3 bands that respects TOS values    
                node.cmd(f"sudo tc qdisc add dev {intf} parent 1:1 pfifo_fast")
    
    # This function takes two mininet nodes A and B that are connected by a link
    # and returns the name of the interface at node B that is connected to that link
    def findInterface(self, nodeA, nodeB):
        # Iterate through all connections (links) of nodeB
        for intf in nodeB.intfList():
            # Get the link connected to the current interface
            link = intf.link
            # Check if the link is connected to nodeA
            if link.intf1.node == nodeA or link.intf2.node == nodeA:
                # Return the name of the interface at nodeB
                return intf.name
        return None
    
    def initiatePath(self, path, label):
        for i in range(1, len(path)-1):
            if(len(path) <= 3):
                # length == 2: border routers are directly connected: no need to specify a path
                # length == 3: border routers are connected through a single router: no need to specify a path since border routers have only one interface
                break
            node = self.net.getNodeByName(path[i])
            prev_node = self.net.getNodeByName(path[i-1])
            ingress_intf = self.findInterface(prev_node, node)
            next_node = self.net.getNodeByName(path[i+1])
            egress_intf = self.findInterface(next_node, node)
            #node.cmd(f"sysctl -w net.mpls.conf.{ingress_intf}.input=1") -> not encessary
            if i == len(path)-2:
                next_hop_intf = self.findInterface(node, next_node)
                new_dst_mac = next_node.MAC(next_hop_intf)
                new_src_mac = node.MAC(egress_intf)
                
                node.cmd(f"tc qdisc add dev {ingress_intf} handle ffff: ingress")
                node.cmd(f"tc filter add dev {ingress_intf} protocol mpls_uc parent ffff: flower \
                        mpls_label {label} \
                        action mpls pop protocol ip \
                        action mirred egress redirect dev {egress_intf}")
                # TODO: Later the qdisc should be created in the start_simulation file when adding link attributes manually
                # Currently link attributes don't work together with colibri paths
                node.cmd(f"tc qdisc add dev {egress_intf} root handle 1: prio")
                node.cmd(f"tc filter add dev {egress_intf} protocol ip parent 1:0 prio 1 flower \
                         ip_tos 0x10/0xff \
                         action pedit ex munge eth dst set {new_dst_mac} \
                         action pedit ex munge eth src set {new_src_mac}")
            else:
                if i == 1:
                    node.cmd(f"tc qdisc add dev {ingress_intf} handle ffff: ingress")
                    node.cmd(f"tc filter add dev {ingress_intf} protocol ip parent ffff: flower \
                                ip_tos 0x10/0xff \
                                action mpls push protocol mpls_uc label {label}  \
                                action mirred egress redirect dev {egress_intf}")
                else:
                    node.cmd(f"tc qdisc add dev {ingress_intf} handle ffff: ingress")
                    node.cmd(f"tc filter add dev {ingress_intf} protocol mpls_uc parent ffff: flower \
                                mpls_label {label} \
                                action mirred egress redirect dev {egress_intf}")