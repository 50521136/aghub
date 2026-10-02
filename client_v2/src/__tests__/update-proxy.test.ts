import { describe, expect, it } from 'vitest';

import { proxyResultsSorted } from 'panel/stores/update';
import type { UpdateProxyTest } from 'panel/api/model';

const node = (
    host: string,
    latency: number,
    apiOK: boolean,
    downloadOK: boolean,
): UpdateProxyTest => ({
    url: `https://${host}`,
    host,
    latency_ms: latency,
    api_ok: apiOK,
    download_ok: downloadOK,
});

describe('proxyResultsSorted', () => {
    it('returns an empty array for no results', () => {
        expect(proxyResultsSorted(null)).toEqual([]);
        expect(proxyResultsSorted([])).toEqual([]);
    });

    it('puts the usable proxies first and the fastest at the top', () => {
        const results = [
            node('dead.example', 0, false, false),
            node('slow.example', 900, false, true),
            node('fast.example', 20, true, true),
            node('medium.example', 300, true, true),
            node('api-only.example', 50, true, false),
        ];

        const sorted = proxyResultsSorted(results);

        expect(sorted.map((r) => r.host)).toEqual([
            'fast.example',
            'api-only.example',
            'medium.example',
            'slow.example',
            'dead.example',
        ]);
    });

    it('does not modify the input', () => {
        const results = [node('b.example', 10, true, true), node('a.example', 5, true, true)];

        proxyResultsSorted(results);

        expect(results.map((r) => r.host)).toEqual(['b.example', 'a.example']);
    });
});
