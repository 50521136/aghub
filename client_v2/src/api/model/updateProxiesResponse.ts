import type { UpdateProxyNode } from './updateProxyNode';

/**
 * The built-in acceleration proxies and the one in use.
 */
export interface UpdateProxiesResponse {
    proxies?: UpdateProxyNode[];

    /** The prefix the updater is using now. */
    current?: string;

    /** The prefix saved in the settings. */
    stored?: string;
}
