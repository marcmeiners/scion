# INTRA-AS SIMULATION

## Requirements

- Install SCION according to the official documentation,
    see [here](../doc/build/setup.rst) for detailed instructions.

- Clone SCION-Apps repository (<https://github.com/netsec-ethz/scion-apps>)
    and build it according to their README.md

## Installation

- run install_deps.sh

    ```bash
    ./install_deps.sh
    ```

    - this will install routing protocols and other dependencies needed to run the intra-AS simulation

## Usage

- Whole usage can be done with the helper script `scion-intra.sh` in the root directory of this repo.
- Automatically create AS configuration file from SCION topology file.

    ```bash
    ./scion-intra.sh create_config -i <SCION-topo-config-file> [other options]
    ./scion-intra.sh create_config -i topology/tiny4.topo
    ```

- Build SCION Proto with intra-AS simulation enabled:

    ```bash
    ./scion-intra.sh <AS-config-file> <SCION-topo-config-file> [other-SCION-topology-flags]
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
