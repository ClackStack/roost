// Everything that must change when Roost goes public lives here.
export const version = 'v0.1.0';
export const repo = 'https://github.com/paulhovey/roost';
const releases = `${repo}/releases/download/${version}`;

export type Platform = { id: string; label: string; file: string };

// File names match `make release`.
export const platforms: Platform[] = [
	{ id: 'linux-amd64', label: 'Linux x86-64', file: `roost-${version}-linux-amd64` },
	{ id: 'linux-arm64', label: 'Linux ARM64', file: `roost-${version}-linux-arm64` },
	{ id: 'darwin-arm64', label: 'macOS Apple silicon (untested)', file: `roost-${version}-darwin-arm64` },
	{ id: 'darwin-amd64', label: 'macOS Intel (untested)', file: `roost-${version}-darwin-amd64` },
	{ id: 'windows-amd64', label: 'Windows x86-64 (untested)', file: `roost-${version}-windows-amd64.exe` }
];

export const downloadUrl = (p: Platform) => `${releases}/${p.file}`;
export const checksumsUrl = `${releases}/SHA256SUMS`;
export const securityUrl = `${repo}/blob/main/SECURITY.md`;
export const readmeUrl = `${repo}#readme`;
export const exampleConfigUrl = `${repo}/blob/main/roost.example.toml`;

// Matches LICENSE in the repo. Drop "(not final)" once the owner confirms it.
export const licence = 'Apache-2.0 (not final)';
