# Twisty

Twisty finds and scores twisty roads (roads with sustained curvature that make them enjoyable to drive or ride) from OpenStreetMap data, and builds routes through them.

## Language

**Tile**:
A rectangular geographic area, identified by its south/west corner, used to chunk map-data fetches from Overpass into bounded, cacheable requests.
_Avoid_: Bounding box, region, chunk

**Way**:
An OSM highway segment with its tags and ordered geometry, as returned by Overpass. The raw unit of input to the scoring pipeline, before curvature scoring is applied.
_Avoid_: Road segment, edge

**ScoredSegment**:
The curvature score for a single stretch of road between two consecutive nodes of a Way, carrying its circumradius, tier, weight, length, and resulting score.
_Avoid_: Curve, node pair

**ScoredWay**:
A Way after curvature scoring has been applied to each of its segments. Holds the Way's ID, tags, and its ScoredSegments.
_Avoid_: Scored road

**RoadCollection**:
A named group of contiguous ScoredSegments aggregated across one or more Ways that share a name or ref tag, representing a single real-world road (or a stretch of one, if a long straight gap split it into sub-collections). The unit that final scores, filters, and rendered output (KML, build UI) operate on.
_Avoid_: Road group, aggregated road

**Leg**:
The portion of a routed path between two consecutive Waypoints, as returned by a routing provider (OSRM, Valhalla). Multi-waypoint routes decode and stitch one Leg per waypoint pair.
_Avoid_: Route segment, hop

**Waypoint**:
A point a route is built through — either a stop the user has placed when building a route, or a `<wpt>` element written to an exported GPX file.
_Avoid_: Stop, point, marker

**Twistiness**:
The qualitative property a RoadCollection has when it accumulates a meaningful curvature score — the thing the whole scoring pipeline exists to measure and rank roads by.
_Avoid_: Curviness, windiness

**Score / score_per_km**:
Score is a RoadCollection's total curvature score: the sum of each ScoredSegment's length-times-weight contribution. Score_per_km normalizes that total by the collection's length, so roads of different lengths can be compared and color-graded consistently.
_Avoid_: Rating, curvature index

**Deflection**:
The cumulative heading change a road accumulates within a fixed look-ahead window (2400 m). Used post-aggregation to zero out segments that are minor wobbles in an otherwise straight road, rather than genuine curves.
_Avoid_: Heading change filter, wobble

**Tier**:
One of five bands (0–4) a ScoredSegment is assigned to based on its circumradius, from straight (tier 0, zero weight) to tightest (tier 4, highest weight). Tiers determine how much a segment's length contributes to its Score.
_Avoid_: Bucket, class, grade

**Circumradius**:
The radius of the circle passing through three consecutive geometry points of a Way. A smaller circumradius means a tighter curve; it's the geometric measurement that drives tier assignment. Straight (collinear) points yield an infinite circumradius.
_Avoid_: Curve radius, radius of curvature

**Hard filter**:
The stage that removes Ways outright before scoring — unpaved surfaces, private/restricted access, and non-motor-vehicle highway types (tracks, footways, cycleways, etc.) never enter the curvature pipeline.
_Avoid_: Pre-filter, exclusion filter

**Viewport**:
The visible map area in the build UI, expressed as a south/west/north/east bounding box. Drives on-demand tile fetching and scoring for only the roads currently in view, capped at a maximum number of tiles per request.
_Avoid_: Map bounds, view area
