#!/usr/bin/env python3
import os
import configparser
from pathlib import Path

from python.topology.supervisor import (
    SUPERVISOR_CONF,
)
from python.lib.defines import (
    GEN_PATH,
)


def clean_up_scion():
    """Kill SCION processes."""
    os.system('sudo pkill -9 scion-bwtestserver')
    os.system('sudo pkill -9 scion-bwtestclient')

    SCION_PATH = Path('/home/scion/Documents/scion/')
    SUPERVISORD_FILE = Path(SCION_PATH, GEN_PATH, SUPERVISOR_CONF)
    if not SUPERVISORD_FILE.exists():
        print('No supervisor config file found - fallback to less safe method')
        answer = input('Continue? [y/N] ').lower()
        if answer not in ('y', 'yes'):
            print('Aborting')
            return

        commands = ['bin/posix-router --config ',
                    'bin/co --config ',
                    'bin/cs --config ',
                    'bin/daemon --config ',
                    'bin/dispatcher --config ',
                    ]
        for command in commands:
            os.system(f'sudo pkill -f "{command}"')
    else:
        supervisor_config = configparser.ConfigParser()
        supervisor_config.read(SUPERVISORD_FILE)

        for section in supervisor_config.sections():
            scion_command = supervisor_config[section].get('command', None)
            os.system(f'pkill -f "{scion_command}"')


def clean_up():
    """Clean up junk which might be left over from old runs"""

    os.system('sudo pkill -9 zebra')
    os.system('sudo pkill -9 ospf')
    os.system('sudo rm -rf /var/tmp/SCION_INTRA_*')
    os.system('sudo rm -rf /var/tmp/frr/')

    clean_up_scion()

    os.system('sudo mn -c')


def main():
    clean_up()


if __name__ == '__main__':
    main()
