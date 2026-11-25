import preact from "@preact/preset-vite";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";

// The Go server answers the API and the public link routes in development
// (make dev runs both).
const server = "http://127.0.0.1:8080";

export default defineConfig({
	plugins: [preact(), tailwindcss()],
	server: {
		port: 5173,
		proxy: {
			"/api": server,
			"/healthz": server,
			"/r/": server,
			"/v/": server,
			"/brief.json": server,
			"/feed.xml": server,
		},
	},
	build: {
		outDir: "dist/app",
		emptyOutDir: true,
		assetsInlineLimit: 0,
	},
});
