#!/bin/sh
set -eu

# This namespace belongs only to this search worker. Docker-local connections
# remain available; private search API/metrics traffic from the tailnet uses Russia.
# The raw hook runs before tailscaled's filter-table ACCEPT rules.
router=${SEARCH_ROUTER_ADDRESS:?Set the router tailnet IPv4 address}
iptables -t raw -C PREROUTING -i tailscale0 -p tcp -m multiport --dports 50054,9103 ! -s "$router" -j DROP 2>/dev/null ||
    iptables -t raw -I PREROUTING 1 -i tailscale0 -p tcp -m multiport --dports 50054,9103 ! -s "$router" -j DROP
ip6tables -t raw -C PREROUTING -i tailscale0 -p tcp -m multiport --dports 50054,9103 -j DROP 2>/dev/null ||
    ip6tables -t raw -I PREROUTING 1 -i tailscale0 -p tcp -m multiport --dports 50054,9103 -j DROP
exec /usr/local/bin/containerboot
