#!/usr/bin/env bash

# install OSPF/Zebra and other routing protocols
# add GPG key
curl -s https://deb.frrouting.org/frr/keys.asc | sudo apt-key add -

# possible values for FRRVER: frr-6 frr-7 frr-8 frr-stable
# frr-stable will be the latest official stable release
FRRVER="frr-stable"
echo deb https://deb.frrouting.org/frr $(lsb_release -s -c) $FRRVER | sudo tee -a /etc/apt/sources.list.d/frr.list

# update and install FRR
sudo apt update
sudo apt install -y frr \
         frr-pythontools \
         python3-pip

sudo pip3 install -r requirements.txt