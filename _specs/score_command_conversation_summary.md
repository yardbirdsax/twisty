# Score Command — Conversation Summary

## What we discussed

Josh wants to add a `score` command to Twisty that discovers and ranks twisty
roads in a geographic area, inspired by the
[Curvature project](https://github.com/adamfranco/curvature). This is a
learning exercise — Josh is driving the design, not being handed solutions.

## Current state of the codebase

- `ScoreRoute` in `route/score.go` scores candidate routes using angular
  density (heading change per km) and indirectness. This is simpler than
  Curvature's approach and suited for comparing a few routes, not bulk road
  discovery.
- `quality/overpass.go` already fetches OSM ways for a bounding box and has
  quality filtering functions (`IsDisqualifying`, `PenaltyFactor`,
  `HighwayTypePenalty`, `LocalBonus`). The data fetching and hard/soft filter
  logic can be reused.

## Agreed-upon pipeline for the score command

1. **Fetch** OSM road data for a region via Overpass (with caching)
2. **Hard filter** — remove unpaved, private, non-motor-vehicle roads
3. **Score segments** — circumradius → tier → weight × segment length
4. **Filter deflections** — zero out doglegs and intersection jogs
5. **Aggregate** — group ways by road name, split at long straight stretches,
   sum scores
6. **Soft penalties** — adjust aggregated scores for highway type, etc.

Full algorithm details are in `_specs/curvature_pipeline.md`.

## Concepts Josh understands well

- Why circumradius is better than angular density for bulk scoring
- Why tiering suppresses OSM node density noise
- Why segment score = length × weight (normalizes for node density)
- Why deflection filtering is needed (doglegs, intersection jogs)
- Why splitting at straight stretches prevents score dilution
- Why soft penalties go after aggregation (need the true road-level score first)
- Hard vs soft filtering distinction maps to existing code

## What's left to discuss

- Caching strategy for Overpass data (format, staleness, granularity)
- Overpass vs PBF extracts tradeoff (raised but deferred)
- Concurrency design (Josh says he has a good understanding already)
- CLI design for the `score` subcommand
- Josh wants to be quizzed on the pipeline for retention
