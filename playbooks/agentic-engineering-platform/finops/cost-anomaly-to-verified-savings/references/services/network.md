# Network cost analysis

## Cost drivers

Separate internet egress, cross-zone and cross-region transfer, NAT or gateway processing, load balancers, public/static IPs, private connectivity, transit/VPN, CDN, DNS, and service-specific transfer charges. Preserve both ends of a charge when attribution data permits.

## Evidence to collect

Collect topology, routes, zones/regions, source/destination service and owner, sanitized flow or billing dimensions, bytes and request counts, NAT/LB metrics, cache hit rate, payload size, latency, errors, availability, failover paths, and residency/security constraints.

## Candidate checks

- Attribute large flows and distinguish expected demand from accidental routing or retry amplification.
- Model safe co-location, private service endpoints, cache/CDN changes, or payload compression.
- Review unused IPs, load balancers, gateways, and stale rules only after dependency and ownership confirmation.
- Consider topology consolidation only with availability, scaling, and failure-domain analysis.

## Guardrails and verification

Require architecture/security review for route, trust-boundary, residency, or failure-domain changes. Include appliance, endpoint, request, cache-fill, and migration costs in the model. Verify traffic path, bytes, cache behavior, latency, errors, availability, security controls, and the full set of billed network components.
