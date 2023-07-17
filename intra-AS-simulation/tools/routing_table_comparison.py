import os
import json

def compare_json_files(path1, path2):
    with open(path1, 'r') as file1:
        data1 = json.load(file1)
    with open(path2, 'r') as file2:
        data2 = json.load(file2)
    if data1 == data2:
        print("\nThe routing tables are equal.")
    else:
        print("The routing tables are not equal.")

def client():    
    while True:
        print('Compare routing tables of two different ASes that share the same topology \n')
        print('--------------------------------------------------\n')

        # Get the directory containing this script
        script_directory = os.path.dirname(os.path.abspath(__file__))

        # Get the gen directory
        gen_directory = os.path.join(script_directory, '../../gen')

        # Normalize the path (resolve '..')
        gen_directory = os.path.normpath(gen_directory)

        # Get the List of ASes
        as_list = [name for name in os.listdir(gen_directory) if name.startswith('AS') and os.path.isdir(os.path.join(gen_directory, name))]

        # Print out the ASes
        for index, directory in enumerate(as_list):            
            print(str(index) + ": " + directory)
        
        as1 = input('Enter number of the first AS:')
        if not 0 <= int(as1) < len(as_list):
            print('Invalid AS number. Try again.\n')
            continue
        as1 = as_list[int(as1)]
        as1_file = os.path.join(gen_directory, as1, 'routing-tables.json')
        if not os.path.exists(as1_file):
            print('Routing Table JSON file does not exist for this AS.\n')
            print('Make sure to first build and run your network and then export the routing tables using the intra-AS client.\n')
            continue
        as2 = input('Enter number of the second AS:')
        if not 0 <= int(as2) < len(as_list):
            print('Invalid AS number. Try again.\n')
            continue
        as2 = as_list[int(as2)]
        as2_file = os.path.join(gen_directory, as2, 'routing-tables.json')
        if not os.path.exists(as2_file):
            print('Routing Table JSON file does not exist for this AS.\n')
            print('Make sure to first build and run your network and then export the routing tables using the intra-AS client.\n')
            continue
        compare_json_files(as1_file, as2_file)
        break

def main():
    client()

if __name__ == "__main__":
    main()