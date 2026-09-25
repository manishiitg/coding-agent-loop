# Diagrams

Use a precise, readable figure when a child needs to see a shape, graph or
number line. Check labels, values and proportions in the rendered page before
handing it over. Use real data; do not invent numbers for a chart.

## JSXGraph reference

JSXGraph is available in SparkQuill pages without a remote `<script>` tag.
It is useful for geometry because it computes points, arcs and labels from
mathematical coordinates. Give each figure a sized `jxgbox` and a unique ID:

```html
<div id="fig1" class="jxgbox" style="width:340px;height:280px"></div>
<script>
window.addEventListener('load', function () {
  const board = JXG.JSXGraph.initBoard('fig1', {
    boundingbox: [-1, 6, 9, -1], axis: false,
    showNavigation: false, showCopyright: false,
    keepAspectRatio: true
  });
  const A = board.create('point', [1, 1], {name:'A', fixed:true});
  const B = board.create('point', [5, 1], {name:'B', fixed:true});
  const C = board.create('point', [7, 4], {name:'C', fixed:true});
  board.create('segment', [B, A]);
  board.create('segment', [B, C]);
  board.create('angle', [C, B, A], {name:'∠ABC'});
});
</script>
```

For `angle`, the middle point is the vertex; point order determines which
side the arc marks. For a circle, `board.create('circle', [centre, radius])`
uses a point and radius. For a function graph, use `axis:true` and
`board.create('functiongraph', [x => x * x])`. Keep an equal aspect ratio for
geometry so circles and right angles are not distorted.

A different drawing method is fine when it produces an accurate figure. See
`html-design.md` → “Check a figure before you finish” for the viewer URL used
to inspect a page with JSXGraph.
