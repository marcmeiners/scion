# INTRA-AS SIMULATION

## Installation
For the intra-AS simulation framework to work properly, a fresh Ubuntu 20.04 system is required. 

### Create scion user and clone SCION

    $ adduser scion
    $ usermod -aG sudo scion
    $ su scion
    $ cd
    $ git clone https://github.com/marcmeiners/scion
    $ cd scion
    $ git checkout mmeiners/intra_AS_simulation
    $ sudo apt update
    $ ./tools/install_bazel
    $ ./env/deps

### Install Docker

    $ sudo snap refresh snapd
    $ sudo snap install docker
    $ sudo apt-get install docker-compose
    $ sudo usermod -a -G docker scion

### Install Python

    $ sudo apt-get install python    

### Install Supervisor

    $ sudo apt-get install supervisor -y
    $ pip install supervisor-wildcards

### Python modules

    $ pip3 install networkx

### Install and build Bazel

    $ cd ..
    $ wget https://github.com/bazelbuild/bazelisk/releases/download/v1.8.1/bazelisk-linux-amd64
    chmod +x bazelisk-linux-amd64
    $ sudo mv bazelisk-linux-amd64 /usr/local/bin/bazel
    $ which bazel
    $ cd scion
    $ sudo ./scion.sh bazel_remote

### Build SCION

    $ ./scion.sh topology

### Install SCION Apps

> Make sure SCION apps is cloned into the same folder as SCION: \
> -folder \
> --scion \
> --scion-apps

    $ cd
    $ git clone https://github.com/marcmeiners/scion-apps
    $ cd scion-apps
    $ git checkout mmeiners/intra_AS_simulation
    $ sudo snap install go --channel=1.16/stable --classic
    $ sudo apt-get install -y libpam0g-dev
    $ make setup_lint
    $ make
    $ make install

### Install SCION intra requirements

    $ cd ~/scion
    $ ./intra-AS-simulation/install_deps.sh
    $ sudo reboot
## Usage

- Whole usage can be done with the helper script `scion-intra.sh` in the root directory of this repo.
- Automatically create AS configuration file from SCION topology file.

    ```bash
    ./scion-intra.sh create_config -i <SCION-topo-config-file> [other options]
    ./scion-intra.sh create_config -i topology/tiny4.topo
    ```

- Build SCION Proto with intra-AS simulation enabled:

    ```bash
    ./scion-intra.sh build <AS-config-file> <SCION-topo-config-file> [other-SCION-topology-flags]
    ```

- Then run the simulation:

    ```bash
    export SCION_APPS_PATH=path-to-SCION-apps-directory
    ./scion-intra.sh run
    ```

    - It is also possible to enter the SCION-apps directory directly:

        ```bash
        ./scion-intra.sh run --apps path-to-SCION-apps-directory
        ```

- Cleanup:
    - only intra AS simulation:

        ```bash
        ./scion-intra.sh clean_intra
        # OR
        ./scion-intra.sh clean
        ```

    - or all:

        ```bash
        ./scion-intra.sh clean_all
        ```
