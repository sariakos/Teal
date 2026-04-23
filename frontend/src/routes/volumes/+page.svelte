<!--
  Platform-wide volumes browser. Lists every named volume on the host so
  admins can clean up orphans. The same VolumesPanel component is reused
  on each App's detail page (filtered to that app's prefix).
-->
<script lang="ts">
	import VolumesPanel from '$lib/components/VolumesPanel.svelte';
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-2xl font-semibold text-zinc-900">Volumes</h1>
		<p class="mt-1 text-sm text-zinc-500">All Docker named volumes on this host.</p>
	</div>
	<VolumesPanel />
</div>
