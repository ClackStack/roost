// Everything that must change when Roost cuts a release lives here.
export const version = 'v0.1.0';
export const repo = 'https://github.com/ClackStack/roost';
const releases = `${repo}/releases/download/${version}`;

export type Platform = { id: string; label: string; file: string };

// File names match `make release` (PLATFORMS in Makefile).
export const platforms: Platform[] = [
	{ id: 'linux-amd64', label: 'Linux x86-64', file: `roost-${version}-linux-amd64` },
	{ id: 'linux-arm64', label: 'Linux ARM64', file: `roost-${version}-linux-arm64` }
];

export const downloadUrl = (p: Platform) => `${releases}/${p.file}`;
export const checksumsUrl = `${releases}/SHA256SUMS`;
export const securityUrl = `${repo}/blob/main/SECURITY.md`;
export const readmeUrl = `${repo}#readme`;
export const exampleConfigUrl = `${repo}/blob/main/roost.example.toml`;

export const licence = 'Apache-2.0';
