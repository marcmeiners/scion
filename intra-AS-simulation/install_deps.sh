#!/usr/bin/env bash

# INSTALL commands taken from: https://deb.frrouting.org/

# add GPG key
curl -s https://deb.frrouting.org/frr/keys.asc | sudo apt-key add -

# possible values for FRRVER: frr-6 frr-7 frr-8 frr-stable
# frr-stable will be the latest official stable release
FRRVER="frr-stable"
echo deb https://deb.frrouting.org/frr $(lsb_release -s -c) $FRRVER | sudo tee -a /etc/apt/sources.list.d/frr.list

# update and install FRR
sudo apt update
sudo apt install frr frr-pythontools

# install intra-AS dependencies
sudo apt install -y python3-pip
python3 -m pip install pip --upgrade
python3 -m pip install -r requirements.txt

