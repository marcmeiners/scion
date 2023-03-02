#!/usr/bin/env python3

import networkx as nx
from networkx import NetworkXException
import matplotlib.pyplot as plt
from mininet.log import info, output
from mininet.cli import CLI

from test_SCION_in_mininet import test_SCION_ping, test_SCION_bwtest_client_server


def plot_topology(AS):
    """Plot the intra-AS topology

    Draws all nodes and edges of internal topology.
    Also annotates edges with link properties.
    """
    # set figure title
    plt.figure(num=f'Intra-AS topology of AS: {AS.ISD_AS_id}')
    G = AS.graph
    try:
        pos = nx.planar_layout(G)
    except NetworkXException:
        pos = nx.spring_layout(G)
    # draw nodes
    nx.draw_networkx_nodes(G, pos, node_color='#8691fc')
    # add node names
    nx.draw_networkx_labels(G, pos)
    ax = plt.gca()
    for e in G.edges:
        # manually connect 2 nodes
        # this is needed, since we can have multiple edges between 2 nodes
        ax.annotate("",
                    xy=pos[e[0]], xycoords='data',
                    xytext=pos[e[1]], textcoords='data',
                    arrowprops=dict(arrowstyle="-", alpha=0.6,
                                    shrinkA=10, shrinkB=10,
                                    # creates line with a specific curvature/radius
                                    connectionstyle=f"arc3,rad={-0.1*e[2]}",
                                    ),
                    )
        # add label to edge
        label = G.get_edge_data(*e)['label']
        # text position: center of edge   + correction when multiple edges between two nodes
        text_pos = (pos[e[0]] + pos[e[1]] + [0.08*e[2], 0.08*e[2]]) / 2
        ax.annotate(label, xy=text_pos, xycoords='data',
                    xytext=text_pos, textcoords='data',
                    fontsize=10)

    plt.axis('off')
    plt.show()


def AS_CLI(networks, ASes, AS):
    """CLI for interacting with the AS"""
    while True:
        try:
            output('--------------------------------------------------\n')
            output(f'In the CLI of AS: \t {AS.ISD_AS_id} \n')
            output('[0] Back to main menu\n')
            output('[1] Test SCION ping\n')
            output('[2] Start SCION bandwidth test\n')
            output('[3] Start Mininet CLI\n')
            output('[4] Draw intra-AS topology\n')
            option = input('Enter option: ').strip()
            if option == '0':
                break
            elif option == '1':
                info('### Testing SCION ###\n')
                try:
                    test_SCION_ping(ASes, AS)
                except Exception as e:
                    output(f'Test failed: {e}\n')
            elif option == '2':
                info('### Starting SCION bwtest ###\n')
                try:
                    test_SCION_bwtest_client_server(ASes, networks, AS)
                except Exception as e:
                    output(f'Test failed: {e}\n')
            elif option == '3':
                info('### Starting Mininet CLI ###\n')
                output('### Press CTRL+D or type `quit`, `exit` to exit Mininet CLI ###\n')
                CLI(AS.net)
            elif option == '4':
                info('### Drawing intra AS topology ###\n')
                try:
                    plot_topology(AS)
                except Exception as e:
                    output(f'Drawing failed: {e}\n')
            else:
                output('Invalid option\n')
        except (Exception, KeyboardInterrupt):
            continue


if __name__ == '__main__':
    pass
