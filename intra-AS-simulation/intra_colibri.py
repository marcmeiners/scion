class IntraColibri(object):
    def __init__(self, AS):
        self.FULL_NAME = AS.FULL_NAME
        self.net = AS.net
        self.SCION_PATH = AS.SCION_PATH
        self.nodes = self.net.hosts

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