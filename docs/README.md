# Stint documentation

The repository root contains the product overview and quick-start material. This
directory contains the longer-lived references, grouped by how they are used.

## Guides

- [Authentication](guides/authentication.md) explains product, provider, and
  repository credentials.
- [Spark integration](guides/spark.md) explains the evidence boundary.

## Reference

- [Architecture](reference/architecture.md) describes the control-plane shape.

## Operations and planning

- [Next Stint](operations/next-stint.md) is the current operational checklist.
- [Roadmap](planning/roadmap.md) tracks planned product stages.

## Historical records

- [Phase 1](history/phase-1.md) records the local foundation work.
- [Phase 2](history/phase-2.md) records live marketplace-planning findings.

Run reports, experiments, incident records, and implementation plans belong in
their feature's documentation PR under a topic-specific `history/` directory.
They should not be added to the documentation root or combined with an
implementation PR unless the document is necessary to operate the changed
behavior.
