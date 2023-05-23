from pathlib import Path


class Zebra(object):
    """Class for Zebra daemon"""

    def __init__(self, FULL_NAME, SCION_PATH):
        self.FULL_NAME = FULL_NAME
        self.SCION_PATH = SCION_PATH
        self.config_path = Path(self.SCION_PATH, f'gen/zebra/{self.FULL_NAME}')
        self.config_path.mkdir(parents=True, exist_ok=True)

    def create_config(self, node):
        """Create zebra config file for this node.

        Defines on which interface the zebra daemon should listen.
        """
        name = node.name

        zebra_config_file = Path(self.config_path, f'zebra_{name}.conf')

        config = f"hostname {name}"
        config += "\npassword scion"

        for intf in node.intfList():
            config += f"\n\ninterface {intf.name}"
            config += f"\n\tip address {intf.IP()}/{intf.prefixLen}"
            config += "\n\tno shutdown"

        with open(zebra_config_file, mode='w') as f:
            f.writelines(config)

        return zebra_config_file


class OSPF(object):
    """Class for OSPF daemon"""

    def __init__(self, AS):
        self.FULL_NAME = AS.FULL_NAME
        self.net = AS.net
        self.SCION_PATH = AS.SCION_PATH
        self.Zebra = Zebra(self.FULL_NAME, self.SCION_PATH)
        self.nodes = self.net.hosts
        self.temp_path = Path(f'/var/tmp/SCION_INTRA_{self.FULL_NAME}/')
        self.temp_path.mkdir(parents=True, exist_ok=True)
        self.config_path = Path(self.SCION_PATH, f'gen/ospf/{self.FULL_NAME}')
        self.config_path.mkdir(parents=True, exist_ok=True)

    def start(self):
        """Start OSPF daemon on each node"""
        Path(self.SCION_PATH, f'logs/{self.FULL_NAME}/').mkdir(parents=True, exist_ok=True)

        for node in self.nodes:
            name = node.name
            socket = Path(self.temp_path, f'{name}_quagga.api')
            pid_zebra = Path(self.temp_path, f'{name}_zebra.pid')
            pid_ospf = Path(self.temp_path, f'{name}_ospf.pid')
            zebra_config = self.Zebra.create_config(node)
            ospf_config = self.create_config(node)

            node.cmd(f'cd {self.SCION_PATH} ; \
                    /usr/lib/frr/zebra -u root -d -f {zebra_config} -z {socket} -i {pid_zebra} \
                    --log-level debug --log file:logs/{self.FULL_NAME}/zebra_{name}.log 2>&1 &')
            node.cmd(f'cd {self.SCION_PATH} ; \
                    /usr/lib/frr/ospfd -u root -d -f {ospf_config} -z {socket} -i {pid_ospf} \
                     --log-level debug --log file:logs/{self.FULL_NAME}/ospf_{name}.log 2>&1 &')

    def stop(self):
        """Stops OSPF daemon on each node + Zebra daemon"""
        for node in self.nodes:
            name = node.name
            socket = Path(self.temp_path, f'{name}_quagga.api')
            pid_zebra = Path(self.temp_path, f'{name}_zebra.pid')
            pid_ospf = Path(self.temp_path, f'{name}_ospf.pid')
            if node.waiting:
                # give time for the process to stop
                node.monitor(timeoutms=2000)
                node.waiting = False
            node.cmd(f'kill -9 $(cat {pid_zebra}) > /dev/null 2>&1')
            node.cmd(f'kill -9 $(cat {pid_ospf}) > /dev/null 2>&1')
            node.cmd(f'rm {pid_zebra} > /dev/null 2>&1')
            node.cmd(f'rm {pid_ospf} > /dev/null 2>&1')
            node.cmd(f'rm {socket} > /dev/null 2>&1')
        node.cmd(f'rm -rf {self.Zebra.config_path} > /dev/null 2>&1')
        node.cmd(f'rm -rf {self.config_path} > /dev/null 2>&1')
        node.cmd(f'rm -rf {self.temp_path} > /dev/null 2>&1')
        node.cmd('rm -rf /var/tmp/frr/ > /dev/null 2>&1')

    def create_config(self, node):
        """Create OSPF config file for this node.

        Defines on which interface the OSPF daemon should listen.
        Calculates OSPF link cost based on the link properties.
        """
        name = node.name
        config_file = Path(self.config_path, f'ospf_{name}.conf')

        config = f"hostname {name}"
        config += "\npassword scion"

        for intf in node.intfList():
            cost = OSPF.calc_cost(intf.params)

            config += f"\n\ninterface {intf.name}"
            config += "\n\tip ospf area 0"
            config += f"\n\tip ospf cost {cost}"

        config += "\n\nrouter ospf"
        config += f"\n\tospf router-id {node.defaultIntf().IP()}"
        config += "\n\tredistribute connected"
        config += "\n\tpassive-interface default"
        # config += "\n\tmpls-te"
        # config += f"\n\tmpls-te router-address {node.defaultIntf().IP()}"

        for intf in node.intfList():
            config += f"\n\tno passive-interface {intf.name}"

        with open(config_file, mode='w') as f:
            f.writelines(config)

        return config_file

    @staticmethod
    def calc_cost(params):
        """Calculate OSPF link cost based on the link properties."""
        max_cost = 65535
        cost = 10
        bw = params.get('bw', None)
        delay = params.get('delay', None)
        loss = params.get('loss', None)
        jitter = params.get('jitter', None)

        if bw is not None:
            # increase cost for low bw
            bw = int(bw)
            if int(bw) < 100:
                cost += int(1000/bw)
        if delay is not None:
            # increase cost for high delay
            raw_delay = delay.split('ms')[0]
            real_delay = int(raw_delay)
            cost += real_delay
        if loss is not None:
            # increase cost for high loss
            cost += int(10*float(loss))
        if jitter is not None:
            raw_jitter = jitter.split('ms')[0]
            # jitter in ms
            real_jitter = int(raw_jitter)
            cost += real_jitter

        return min(cost, max_cost)
    
class ISIS(object):
    def __init__(self, AS):
        self.FULL_NAME = AS.FULL_NAME
        self.net = AS.net
        self.SCION_PATH = AS.SCION_PATH
        self.Zebra = Zebra(self.FULL_NAME, self.SCION_PATH)
        self.nodes = self.net.hosts
        self.temp_path = Path(f'/var/tmp/SCION_INTRA_{self.FULL_NAME}/')
        self.temp_path.mkdir(parents=True, exist_ok=True)
        self.config_path = Path(self.SCION_PATH, f'gen/isis/{self.FULL_NAME}')
        self.config_path.mkdir(parents=True, exist_ok=True)

    def start(self):
        """Start IS-IS daemon on each node"""
        Path(self.SCION_PATH, f'logs/{self.FULL_NAME}/').mkdir(parents=True, exist_ok=True)

        for node in self.nodes:
            name = node.name
            socket = Path(self.temp_path, f'{name}_quagga.api')
            pid_zebra = Path(self.temp_path, f'{name}_zebra.pid')
            pid_isis = Path(self.temp_path, f'{name}_isis.pid')
            zebra_config = self.Zebra.create_config(node)
            isis_config = self.create_config(node)

            node.cmd(f'cd {self.SCION_PATH} ; \
                    /usr/lib/frr/zebra -u root -d -f {zebra_config} -z {socket} -i {pid_zebra} \
                    --log-level debug --log file:logs/{self.FULL_NAME}/zebra_{name}.log 2>&1 &')
            node.cmd(f'cd {self.SCION_PATH} ; \
                    /usr/lib/frr/isisd -u root -d -f {isis_config} -z {socket} -i {pid_isis} \
                     --log-level debug --log file:logs/{self.FULL_NAME}/isis_{name}.log 2>&1 &')

    def stop(self):
        """Stops IS-IS daemon on each node + Zebra daemon"""
        for node in self.nodes:
            name = node.name
            socket = Path(self.temp_path, f'{name}_quagga.api')
            pid_zebra = Path(self.temp_path, f'{name}_zebra.pid')
            pid_isis = Path(self.temp_path, f'{name}_isis.pid')
            if node.waiting:
                # give time for the process to stop
                node.monitor(timeoutms=2000)
                node.waiting = False
            node.cmd(f'kill -9 $(cat {pid_zebra}) > /dev/null 2>&1')
            node.cmd(f'kill -9 $(cat {pid_isis}) > /dev/null 2>&1')
            node.cmd(f'rm {pid_zebra} > /dev/null 2>&1')
            node.cmd(f'rm {pid_isis} > /dev/null 2>&1')
            node.cmd(f'rm {socket} > /dev/null 2>&1')
        node.cmd(f'rm -rf {self.Zebra.config_path} > /dev/null 2>&1')
        node.cmd(f'rm -rf {self.config_path} > /dev/null 2>&1')
        node.cmd(f'rm -rf {self.temp_path} > /dev/null 2>&1')
        node.cmd('rm -rf /var/tmp/frr/ > /dev/null 2>&1')
        node.cmd('rm -rf /var/run/frr')

    def create_net_address(self, node):
        ip = node.defaultIntf().IP()
        parts = ip.split('.')
        area_prefix = "49.0001."
        device_id = ""
        device_id_final = ""
        nsel_suffix = ".00"
        for part in parts:
            device_id += part.zfill(3)
        device_id_final = device_id[0:4] + '.' + device_id[4:8] + '.' + device_id[8:12]
        return area_prefix + device_id_final + nsel_suffix        
        
    def create_config(self, node):
        """Create IS-IS config file for this node.

        Defines on which interface the IS-IS daemon should listen.
        Calculates IS-IS link cost based on the link properties.
        """
        name = node.name
        config_file = Path(self.config_path, f'isis_{name}.conf')

        config = f"hostname {name}"
        config += "\npassword scion"

        for intf in node.intfList():
            cost = ISIS.calc_cost(intf.params)
            config += f"\n\ninterface {intf.name}"
            config += "\n\tip router isis 0"
            config += "\n\tisis circuit-type level-1"
            config += f"\n\tisis metric {cost}"

        config += "\n\nrouter isis 0"
        config += f"\n\tnet {self.create_net_address(node)}"
        config += "\n\t\tis-type level-1"

        with open(config_file, mode='w') as f:
            f.writelines(config)

        return config_file
    
    #TODO: calculate metric/cost according to paper
    @staticmethod
    def calc_cost(params):
        cost = OSPF.calc_cost(params)
        cost = cost / 65535 * 16777214
        return cost