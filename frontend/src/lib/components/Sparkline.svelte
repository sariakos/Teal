<script lang="ts">
	/*
	 * Pure SVG sparkline. No chart library — these only ever render a
	 * single line, and depending on a chart lib for that would be a lot
	 * of bytes for very little.
	 *
	 * Gracefully handles empty / single-point series (renders a flat
	 * baseline) so callers don't have to special-case loading states.
	 */
	interface Props {
		points: number[];
		width?: number;
		height?: number;
		stroke?: string;
		fill?: string;
		min?: number;
		max?: number;
	}
	let {
		points,
		width = 160,
		height = 36,
		stroke = '#0d9488',
		fill = 'rgba(13,148,136,0.10)',
		min,
		max
	}: Props = $props();

	const lo = $derived(min ?? Math.min(...(points.length > 0 ? points : [0])));
	const hi = $derived(max ?? Math.max(...(points.length > 0 ? points : [1])));
	const span = $derived(hi - lo === 0 ? 1 : hi - lo);

	const coords = $derived.by(() => {
		if (points.length === 0) return '';
		if (points.length === 1) {
			const y = height / 2;
			return `0,${y} ${width},${y}`;
		}
		return points
			.map((v, i) => {
				const x = (i / (points.length - 1)) * width;
				const y = height - ((v - lo) / span) * height;
				return `${x.toFixed(1)},${y.toFixed(1)}`;
			})
			.join(' ');
	});

	// Add a baseline so we can fill underneath.
	const fillCoords = $derived(coords ? `${coords} ${width},${height} 0,${height}` : '');
</script>

<svg
	{width}
	{height}
	viewBox={`0 0 ${width} ${height}`}
	role="img"
	aria-label="sparkline"
	class="overflow-visible"
>
	{#if fillCoords}
		<polygon points={fillCoords} {fill} />
	{/if}
	{#if coords}
		<polyline points={coords} {stroke} fill="none" stroke-width="1.5" />
	{/if}
</svg>
