import random
import networkx as nx

class ComputeColibriPaths(object):
    def choose_paths(self, br_pair_to_paths, graph, edge_usages=None, number_of_random_samples=10, demands=None):
        if edge_usages == None:
            edge_usages = {edge:0 for edge in graph.edges}
        br_pair_to_chosen_paths = {}
    

        if demands == None:
            for br_pair, paths in br_pair_to_paths.items():
                print(number_of_random_samples)
                max_usage_to_path_list = {}
                #pick random paths and get their max usage
                sampled_paths = random.sample(paths,min(len(paths),number_of_random_samples))
                for path in sampled_paths:
                    usage = self.get_max_path_usage(path, edge_usages)
                    if not usage in max_usage_to_path_list: #if this is the first path with this max usage
                        max_usage_to_path_list[usage] = [path]
                    else:
                        max_usage_to_path_list[usage].append(path)
                #pick the path with the lowest max usage
                # if more than one path has the same max usage, pick one of them randomly
                path = random.choice(max_usage_to_path_list[min(max_usage_to_path_list.keys())])
                #update edge usages
                for edge in path:
                    edge_usages[edge] += 1
                br_pair_to_chosen_paths[br_pair] = [path] #list because we can have more than one path per br pair
        else:
            sorted_demands = sorted(demands.items(), key=lambda x: x[1], reverse=True) #distribute highest demands first
            for br_pair, demand in sorted_demands:
                paths = br_pair_to_paths[br_pair]
                max_usage_to_path_list = {}
                #pick random paths and get their max usage
                sampled_paths = random.sample(paths,min(len(paths),number_of_random_samples))
                for path in sampled_paths:
                    usage = self.get_max_path_usage(path, edge_usages)
                    if not usage in max_usage_to_path_list: #if this is the first path with this max usage
                        max_usage_to_path_list[usage] = [path]
                    else:
                        max_usage_to_path_list[usage].append(path)
                #pick the path with the lowest max usage
                # if more than one path has the same max usage, pick one of them randomly
                path = random.choice(max_usage_to_path_list[min(max_usage_to_path_list.keys())])
                #update edge usages
                for edge in path:
                    edge_usages[edge] += demand
                br_pair_to_chosen_paths[br_pair] = [path] #list because we can have more than one path per br pair
        return br_pair_to_chosen_paths, edge_usages


    #choose backup paths should be called after choose paths, with the already computed edge_usages
    def choose_backup_paths(self, br_pair_to_paths, br_pair_to_chosen_paths, graph, edge_usages=None, number_of_random_samples=10, demands=None,br_to_pathlist=None):
        if edge_usages == None:
            edge_usages = {edge:0 for edge in graph.edges}
        br_pair_to_disjoint_paths = {}
        for brpair in br_pair_to_chosen_paths.keys():
            br_pair_to_disjoint_paths[brpair] = self.disjoint_paths(br_pair_to_paths[brpair], br_pair_to_chosen_paths[brpair])
        if demands == None:
            for br_pair, paths in br_pair_to_disjoint_paths.items():
                max_usage_to_path_list = {}
                #pick random paths and get their max usage
                sampled_paths = random.sample(paths,min(len(paths),number_of_random_samples))
                for path in sampled_paths:
                    usage = self.get_max_path_usage(path, edge_usages)
                    if not usage in max_usage_to_path_list: #if this is the first path with this max usage
                        max_usage_to_path_list[usage] = [path]
                    else:
                        max_usage_to_path_list[usage].append(path)
                #pick the path with the lowest max usage
                # if more than one path has the same max usage, pick one of them randomly
                path = random.choice(max_usage_to_path_list[min(max_usage_to_path_list.keys())])
                #update edge usages
                for edge in path:
                    edge_usages[edge] += 1
                br_pair_to_chosen_paths[br_pair].append(path) #list because we can have more than one path per br pair
        else:
            sorted_demands = sorted(demands.items(), key=lambda x: x[1], reverse=True) #distribute highest demands first
            for br_pair, demand in sorted_demands:
                paths = br_pair_to_disjoint_paths[br_pair]
                max_usage_to_path_list = {}
                #pick random paths and get their max usage
                sampled_paths = random.sample(paths,min(len(paths),number_of_random_samples))
                for path in sampled_paths:
                    usage = self.get_max_path_usage(path, edge_usages)
                    if not usage in max_usage_to_path_list: #if this is the first path with this max usage
                        max_usage_to_path_list[usage] = [path]
                    else:
                        max_usage_to_path_list[usage].append(path)
                #pick the path with the lowest max usage
                # if more than one path has the same max usage, pick one of them randomly
                path = random.choice(max_usage_to_path_list[min(max_usage_to_path_list.keys())])
                #update edge usages
                for edge in path:
                    edge_usages[edge] += demand
                br_pair_to_chosen_paths[br_pair].append(path) #list because we can have more than one path per br pair
        return br_pair_to_chosen_paths, edge_usages

    def disjoint_paths(self, list_of_paths,path): #returns a new list of paths that share no edge with path
        disjoint_paths = []
        for other_path in list_of_paths:
            disjoint = True
            for edge in path:
                if edge in other_path:
                    disjoint = False
                    break
            if disjoint:
                disjoint_paths.append(other_path)
            
        return disjoint_paths

    def get_max_path_usage(self, path, edge_usages):
        max_usage = 0
        for edge in path:
            if edge_usages[edge] > max_usage:
                max_usage = edge_usages[edge]
        return max_usage

    def path_as_node_list(self, path):
        node_list = []
        for edge in path:
            node_list.append(edge[0])
        node_list.append(path[-1][1])
        return node_list

    def example(self):
        #example usage:
        graph = nx.DiGraph() #your topology
        br_pair_to_paths = {} # the mapping of br tuples ("br1","br2") to their list of possible paths. Each path is a list of edges; each edge is a tuple ("node1","node2")
        demands = {} #the mapping of br tuples ("br1","br2") to their demand (int, can be zero)
        number_of_random_samples = 10 #10 is just a default value, adjust as needed. The higher the number, the more accurate the results, but the longer it takes. anything less than the number of allowed paths per br pair is an optimization at the expense of accuracy

        br_pair_to_chosen_paths, edge_usages = self.choose_paths(br_pair_to_paths, graph, number_of_random_samples, demands)
        br_pair_to_chosen_paths, edge_usages = self.choose_backup_paths(br_pair_to_paths, br_pair_to_chosen_paths, graph, edge_usages, number_of_random_samples, demands)

        #to access the paths chosen for a br pair:
        chosen_paths = br_pair_to_chosen_paths[("source","target")]
        #chosen_paths is a list of two paths, the first one is the primary, the second one is the backup 
        #again, a path is a list of edges, each edge is a tuple ("node1","node2")
        #you can use path_as_node_list to get a list of nodes instead of edges