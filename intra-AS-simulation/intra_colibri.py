class IntraColibri(object):
    
    def __init__(self, AS):
        self.net = AS.net
    
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
    
    def initiatePath(self, path, label, tos):
        lastNodeIP = self.net.getNodeByName(path[-1]).intfList()[0].IP()
        # For paths of lentgh > 2 this loop is executed minimum one time - shorter paths are not allowed anyways
        for i in range(1, len(path)-1):
            node = self.net.getNodeByName(path[i])
            prev_node = self.net.getNodeByName(path[i-1])
            ingress_intf = self.findInterface(prev_node, node)
            next_node = self.net.getNodeByName(path[i+1])
            egress_intf = self.findInterface(next_node, node)
                
            if len(path) == 3:
                break

            # Second last node case
            if i == len(path)-2:
                next_hop_intf = self.findInterface(node, next_node)
                new_dst_mac = next_node.MAC(next_hop_intf)
                new_src_mac = node.MAC(egress_intf)
                
                # At the ingress interface pop the mpls header and redirect the packet
                node.cmd(f"tc qdisc add dev {ingress_intf} handle ffff: ingress")
                node.cmd(f"tc filter add dev {ingress_intf} protocol mpls_uc parent ffff: flower \
                        mpls_label {label} \
                        action mpls pop protocol ip \
                        action mirred egress redirect dev {egress_intf}")
                # The Mac source and destination addresses have to be corrected before the packets returns to normal IP traffic
                # Otherwise the packet gets dropped at the destination BR because its source and destination addresses 
                # are still equal to the first hop before pushing the mpls label
                node.cmd(f"tc filter add dev {egress_intf} protocol ip parent 1: flower \
                        ip_tos {tos}/0xff \
                        action pedit ex munge eth dst set {new_dst_mac} \
                        action pedit ex munge eth src set {new_src_mac}")
            else:
                # Second node case
                if i == 1:
                    # If the packet has a specifc TOS value, push an mpls label and redirect it to the correct interface
                    node.cmd(f"tc qdisc add dev {ingress_intf} handle ffff: ingress")
                    node.cmd(f"tc filter add dev {ingress_intf} protocol ip parent ffff: flower \
                                dst {lastNodeIP} \
                                ip_tos {tos}/0xff \
                                action mpls push protocol mpls_uc label {label}  \
                                action mirred egress redirect dev {egress_intf}")
                # Inner-path node case
                else:
                    # If the packet has a specific mpls label, redirect it acording to the given colibri path
                    node.cmd(f"tc qdisc add dev {ingress_intf} handle ffff: ingress")
                    node.cmd(f"tc filter add dev {ingress_intf} protocol mpls_uc parent ffff: flower \
                                mpls_label {label} \
                                action mirred egress redirect dev {egress_intf}")
                    # Filter that puts mpls packets in correct queue
                    node.cmd(f"tc filter add dev {egress_intf} protocol mpls_uc parent 1: flower \
                            mpls_label {label} \
                            action flowid 1:10")