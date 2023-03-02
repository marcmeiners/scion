# Intra-AS Topology

This file is similarly structured as the SCION topology file.

We have two sections:

- Nodes:
    - Contains a list of node categories, currently supported are:
        Colibri, Control-Service, SCION-Daemon, Borderrouter, Client, Internal-Router
    - In each node category one can define a list of node names
- Links:
    - Connects two nodes together
    - Each link must contain at least one internal-router,
    i.e., it is not possible to connect two SCION nodes directly
    - Multiple links can be defined between two nodes
    - Different link properties can be specified:
        - e.g.: {a: "cl1", b: "r1", bw: 50, loss: 3.5, delay: 100, jitter: 11, mtu: 1300}
        - bw: bandwidth in Mbps (max. 1000) [integer]
        - loss: packet loss in % (0.0 - 100.0) [float]
        - delay: delay in ms [integer]
        - jitter: jitter in ms [integer]
        - mtu: MTU in bytes [integer]

This intra-AS topology file can be assigned to one or more ASes in the AS configuration file.
An AS configuration file can be automatically generated:

```bash
./scion-intra.sh create_config -i <SCION-topo-config-file> -o <output-name-AS-config-file> -t <custom-intra-AS-topology-file>
```

## IMPORTANT NOTE

The AS configuration file maps the name of a border router in the internal topology
to the name/identifier of the border router in the SCION topology.
An intra-AS topology must therefore define at least as many border routers,
as the associated AS has border routers.
But it is possible to define even more border routers in the intra-AS topology.
The unused border router nodes and all links that they are part of get ignored/removed
when actually simulating the intra-AS topology.
The advantage of this approach is that we can use the same intra-AS topology file
for multiple ASes with different numbers of border routers.