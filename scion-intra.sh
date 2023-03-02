#!/bin/bash
export PYTHONPATH=.

cmd_build() {
    # exit when any command fails
    set -e

    if [ -z "$1" ]
    then
        echo "No path to AS configuration supplied!"
        cmd_help
        exit 1
    fi
    if [ ! -f "$1" ]
    then
        echo "Path to AS configuration does not exist!"
        cmd_help
        exit 1
    fi

    if [ -z "$2" ]
    then
        echo "No path to SCION topology configuration supplied!"
        cmd_help
        exit 1
    fi
    if [ ! -f "$2" ]
    then
        echo "Path to SCION topology configuration does not exist!"
        cmd_help
        exit 1
    fi

    sudo rm -rf gen/* gen-cache/* gen-certs/*
    ./scion.sh bazel_remote

    intraConfig=$1
    shift
    topoConfig=$1
    shift

    ./scion.sh topology -n 11.0.0.0/8 -i $intraConfig  --topo-config $topoConfig "$@"
    make build
}


cmd_run(){
    set -e
    sudo -E PYTHONPATH=$PYTHONPATH intra-AS-simulation/start_simulation.py "$@"
}

cmd_create_config(){
    set -e
    python3 intra-AS-simulation/AS_config_creator.py "$@"
}


cmd_clean_intra(){
    ./intra-AS-simulation/clean_up.py
}

cmd_clean_all(){
    cmd_clean_intra
    sudo rm -rf gen/* gen-cache/* gen-certs/*
    ./scion.sh topo_clean
    ./scion.sh clean
}

cmd_help() {
	echo
	cat <<-_EOF
	Usage:
	    $PROGRAM build <AS-config-file> <SCION-topo-config-file> [other SCION topology options]
	        Create topology, configuration, and execution files.
	    $PROGRAM run <AS-configuration-file>
	        Run network.
	    $PROGRAM create_config -i <SCION-topo-config-file> [other options]
	        Creates AS configuration file from SCION topology file
	    $PROGRAM clean_intra
	        Clean intra-AS simulation files.
	    $PROGRAM clean_all
	        Clean all files (SCION + intra).
	    $PROGRAM help
	        Show this text.
	_EOF
}
# END subcommand functions

PROGRAM="${0##*/}"
COMMAND="$1"
shift

case "$COMMAND" in
    help|build|run|create_config|clean_intra|clean_all)
        "cmd_$COMMAND" "$@" ;;
    start) cmd_run "$@" ;;
    clean) cmd_clean_intra ;;
    *)  cmd_help; exit 1 ;;
esac
