const { test, expect } = require('@playwright/test');
const fs = require('fs');
const path = require('path');
const http = require('http');

const webRoot = path.resolve(__dirname, '..', 'internal', 'server', 'web');
const staticRoot = path.join(webRoot, 'static');

const contentTypes = {
  '.css': 'text/css; charset=utf-8',
  '.html': 'text/html; charset=utf-8',
  '.ico': 'image/x-icon',
  '.js': 'application/javascript; charset=utf-8',
  '.png': 'image/png',
  '.svg': 'image/svg+xml',
};

function sendFile(res, filePath) {
  fs.readFile(filePath, (readErr, data) => {
    if (readErr) {
      res.writeHead(readErr.code === 'ENOENT' ? 404 : 500, { 'Content-Type': 'text/plain; charset=utf-8' });
      res.end(readErr.code === 'ENOENT' ? 'Not found' : 'Read error');
      return;
    }

    const ext = path.extname(filePath).toLowerCase();
    res.writeHead(200, { 'Content-Type': contentTypes[ext] || 'application/octet-stream' });
    res.end(data);
  });
}

function createStaticServer() {
  return http.createServer((req, res) => {
    const reqURL = new URL(req.url || '/', 'http://127.0.0.1');
    const reqPath = decodeURIComponent(reqURL.pathname);

    if (reqPath === '/static/runtime.css') {
      res.writeHead(200, { 'Content-Type': 'text/css; charset=utf-8' });
      // Mimic runtime.css variable used by harness panel.
      res.end(':root { --footer-image-height: 200px; }');
      return;
    }

    if (!reqPath.startsWith('/static/')) {
      res.writeHead(404, { 'Content-Type': 'text/plain; charset=utf-8' });
      res.end('Not found');
      return;
    }

    const relativePath = reqPath.slice('/static/'.length);
    const fullPath = path.normalize(path.join(staticRoot, relativePath));
    const allowedRoot = path.normalize(staticRoot + path.sep);
    if (!fullPath.startsWith(allowedRoot)) {
      res.writeHead(403, { 'Content-Type': 'text/plain; charset=utf-8' });
      res.end('Forbidden');
      return;
    }

    sendFile(res, fullPath);
  });
}

test.describe('public theme initial paint', () => {
  let server;
  let baseURL;

  test.beforeAll(async () => {
    server = createStaticServer();
    await new Promise((resolve, reject) => {
      server.once('error', reject);
      server.listen(0, '127.0.0.1', () => resolve());
    });
    const addr = server.address();
    if (!addr || typeof addr === 'string') {
      throw new Error('failed to bind local test server');
    }
    baseURL = `http://127.0.0.1:${addr.port}`;
  });

  test.afterAll(async () => {
    if (!server) return;
    await new Promise((resolve, reject) => {
      server.close((err) => (err ? reject(err) : resolve()));
    });
  });

  test('loads only one selected theme stylesheet set before base CSS', async ({ page }) => {
    await page.addInitScript(() => {
      localStorage.setItem('userSelectedTheme', 'aurora');
    });

    await page.goto(`${baseURL}/static/tests/public-theme-harness.html`, { waitUntil: 'domcontentloaded' });

    const result = await page.evaluate(() => {
      const ids = ['themeVariablesCSS', 'themePublicCSS', 'themeUxCSS'];
      const hrefById = Object.fromEntries(
        ids.map((id) => {
          const el = document.getElementById(id);
          return [id, el ? el.getAttribute('href') : null];
        })
      );
      const countById = Object.fromEntries(
        ids.map((id) => [id, document.querySelectorAll(`#${id}`).length])
      );
      const headChildren = Array.from(document.head.querySelectorAll('link[rel="stylesheet"]'));
      const orderById = Object.fromEntries(
        ids.map((id) => {
          const el = document.getElementById(id);
          return [id, el ? headChildren.indexOf(el) : -1];
        })
      );
      const base = document.getElementById('baseCSS');
      return {
        activeTheme: document.documentElement.getAttribute('data-active-public-theme'),
        hrefById,
        countById,
        orderById,
        baseOrder: base ? headChildren.indexOf(base) : -1,
      };
    });

    expect(result.activeTheme).toBe('aurora');
    expect(result.countById).toEqual({
      themeVariablesCSS: 1,
      themePublicCSS: 1,
      themeUxCSS: 1,
    });
    expect(result.hrefById.themeVariablesCSS).toBe('/static/css/themes/aurora/variables.css');
    expect(result.hrefById.themePublicCSS).toBe('/static/css/themes/aurora/public.css');
    expect(result.hrefById.themeUxCSS).toBe('/static/css/themes/aurora/ux-enhancements.css');
    expect(result.baseOrder).toBeGreaterThanOrEqual(0);
    expect(result.orderById.themeVariablesCSS).toBeGreaterThanOrEqual(0);
    expect(result.orderById.themePublicCSS).toBeGreaterThanOrEqual(0);
    expect(result.orderById.themeUxCSS).toBeGreaterThanOrEqual(0);
    expect(result.orderById.themeVariablesCSS).toBeLessThan(result.baseOrder);
    expect(result.orderById.themePublicCSS).toBeLessThan(result.baseOrder);
    expect(result.orderById.themeUxCSS).toBeLessThan(result.baseOrder);
  });
});
