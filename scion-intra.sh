#!/bin/bash
export PYTHONPATH=.

cmd_build() {
    sudo rm -rf gen/* gen-cache/* gen-certs/*
    ./scion.sh topo_clean
    ./scion.sh bazel_remote

    if [ -z "$1" ]
    then
        echo "No path to intra-AS configuration supplied!"
        cmd_help
        exit 1
    fi
    if [ ! -f "$1" ]
    then
        echo "Path to intra-AS configuration does not exist!"
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

    intraConfig=$1
    shift
    topoConfig=$1
    shift

    ./scion.sh topology -n 11.0.0.0/8 -i $intraConfig  --topo-config $topoConfig "$@"
    make build
}


cmd_run(){
    set -e
    if [ -z "$1" ]
    then
        echo "No path to intra-AS configuration supplied!"
        cmd_help
        exit 1
    fi
    if [ ! -f "$1" ]
    then
        echo "Path to intra-AS configuration does not exist!"
        cmd_help
        exit 1
    fi
    intraConfig=$1
    shift

    sudo -E intra-AS-simulation/start_SCION.py -i $intraConfig "$@"

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
	    $PROGRAM build <intra-AS-config-file> <SCION-topo-config-file> [other SCION topology options]
	        Create topology, configuration, and execution files.
	    $PROGRAM run <intra-AS-configuration-file>
	        Run network.
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
    help|build|run|clean_intra|clean_all)
        "cmd_$COMMAND" "$@" ;;
    start) cmd_run "$@" ;;
    *)  cmd_help; exit 1 ;;
esac