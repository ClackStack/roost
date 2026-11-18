export const prerender = true;

const site = 'https://roost.clackstack.dev';

// The site is a single page; keep this in step with src/routes/.
const sitemap = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>${site}/</loc></url>
</urlset>
`;

export function GET() {
	return new Response(sitemap, {
		headers: { 'Content-Type': 'application/xml' }
	});
}
