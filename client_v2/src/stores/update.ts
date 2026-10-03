import { createStore } from 'solid-js/store';
import { untrack } from 'solid-js';

import {
    aghubUpdateApply,
    aghubUpdateBackup,
    aghubUpdateCheck,
    aghubUpdateProxies,
    aghubUpdateRollback,
    aghubUpdateSetProxy,
    aghubUpdateStatus,
    aghubUpdateTestProxies,
} from 'panel/api/generated';
import type {
    UpdateBackup,
    UpdateCheckResponse,
    UpdateProxyNode,
    UpdateProxyTest,
    UpdateStatus,
    UpdateStatusResponse,
} from 'panel/api/model';

import { addErrorToast, addSuccessToast } from './toasts';
import intl from 'panel/common/intl';

/** pollIntervalMs is the interval of the update state polling. */
const pollIntervalMs = 2000;

/** pollTimeoutMs is how long the polling continues before giving up. */
const pollTimeoutMs = 5 * 60 * 1000;

/**
 * proxyTestBatchSize is the number of proxies tested per request.  The server
 * caps it, and smaller batches let the results arrive progressively.
 */
const proxyTestBatchSize = 40;

type UpdateState = {
    /** currentVersion is the version of the running application. */
    currentVersion: string;

    /** repository is the GitHub repository used for updates. */
    repository: string;

    /** enabled is true if the online update is configured. */
    enabled: boolean;

    /** status is the state of the update in progress. */
    status: UpdateStatus;

    /** check is the result of the last version check. */
    check: UpdateCheckResponse | null;

    /** initialized is true after the first state request. */
    initialized: boolean;

    /** checking is true while the version check is in progress. */
    checking: boolean;

    /** applying is true while the update is in progress. */
    applying: boolean;

    /** backup is the previous version a rollback would restore, if any. */
    backup: UpdateBackup | null;

    /** rollingBack is true while the rollback is in progress. */
    rollingBack: boolean;

    /** proxies is the built-in list of acceleration proxies. */
    proxies: UpdateProxyNode[];

    /** proxy is the acceleration prefix saved in the settings. */
    proxy: string;

    /** proxyCurrent is the prefix the updater is using now. */
    proxyCurrent: string;

    /** testingProxies is true while the acceleration proxies are tested. */
    testingProxies: boolean;

    /** proxyTestDone is the number of proxies tested so far. */
    proxyTestDone: number;

    /** proxyTestTotal is the number of proxies being tested. */
    proxyTestTotal: number;

    /** proxyResults are the results of the last test, if any. */
    proxyResults: UpdateProxyTest[] | null;
};

const emptyStatus: UpdateStatus = {
    progress: -1,
    running: false,
    done: false,
};

const initialState: UpdateState = {
    currentVersion: '',
    repository: '',
    enabled: false,
    status: emptyStatus,
    check: null,
    initialized: false,
    checking: false,
    applying: false,
    backup: null,
    rollingBack: false,
    proxies: [],
    proxy: '',
    proxyCurrent: '',
    testingProxies: false,
    proxyTestDone: 0,
    proxyTestTotal: 0,
    proxyResults: null,
};

const [state, setState] = createStore<UpdateState>(initialState);

export const updateState = untrack(() => state);

/** getUpdateState loads the current update state. */
export const getUpdateState = async () => {
    try {
        const data: UpdateStatusResponse = await aghubUpdateStatus();
        setState({
            currentVersion: data.current_version ?? '',
            repository: data.repository ?? '',
            enabled: data.enabled ?? false,
            status: data.status ?? emptyStatus,
            initialized: true,
        });
    } catch (error) {
        addErrorToast({ error });
    }
};

/** getProxies loads the built-in acceleration proxies and the one in use. */
export const getProxies = async () => {
    try {
        const data = await aghubUpdateProxies();
        setState({
            proxies: data.proxies ?? [],
            proxy: data.stored ?? '',
            proxyCurrent: data.current ?? '',
        });
    } catch (error) {
        addErrorToast({ error });
    }
};

/**
 * setProxy saves the acceleration prefix.  An empty prefix disables the
 * acceleration and makes the updater connect to GitHub directly.
 */
export const setProxy = async (prefix: string) => {
    try {
        const data = await aghubUpdateSetProxy({ proxy: prefix });
        setState({
            proxies: data.proxies ?? [],
            proxy: data.stored ?? '',
            proxyCurrent: data.current ?? '',
        });

        addSuccessToast(
            prefix === ''
                ? intl.getMessage('update_proxy_off_toast')
                : intl.getMessage('update_proxy_saved', { host: prefix }),
        );
    } catch (error) {
        addErrorToast({ error });
    }
};

/**
 * testProxies measures every built-in acceleration proxy from this server, in
 * batches, so that the results arrive as they are produced.
 */
export const testProxies = async () => {
    setState({
        testingProxies: true,
        proxyResults: [],
        proxyTestDone: 0,
        proxyTestTotal: 0,
    });

    try {
        const data = await aghubUpdateProxies();
        const urls = (data.proxies ?? []).map((p) => p.url);
        setState('proxyTestTotal', urls.length);

        const results: UpdateProxyTest[] = [];

        // The batches run one after another so that the results, and the
        // progress, arrive as they are produced.  Running them together does
        // not help: the run is bound by resolving the host names of the
        // proxies that no longer exist.
        for (let i = 0; i < urls.length; i += proxyTestBatchSize) {
            const batch = urls.slice(i, i + proxyTestBatchSize);
            const res = await aghubUpdateTestProxies({ proxies: batch });

            results.push(...(res.results ?? []));

            setState({ proxyResults: [...results], proxyTestDone: results.length });
        }
    } catch (error) {
        addErrorToast({ error });
    } finally {
        setState('testingProxies', false);
    }
};

/**
 * proxyResultsSorted returns the test results with the usable ones first and
 * the fastest at the top.
 */
export const proxyResultsSorted = (results: UpdateProxyTest[] | null) => {
    if (!results) {
        return [];
    }

    return [...results].sort((a, b) => {
        const aOk = Number(Boolean(a.api_ok || a.download_ok));
        const bOk = Number(Boolean(b.api_ok || b.download_ok));

        if (aOk !== bOk) {
            return bOk - aOk;
        }

        return (a.latency_ms ?? 0) - (b.latency_ms ?? 0);
    });
};

/**
 * checkForUpdate queries GitHub for a newer release.  Pass silent to suppress
 * the "already up to date" toast, which is noise when the page checks on its
 * own as soon as it opens.
 */
export const checkForUpdate = async (silent = false) => {
    setState('checking', true);

    try {
        const data = await aghubUpdateCheck();
        setState({ check: data, checking: false });

        if (data.error) {
            addErrorToast({ error: new Error(data.error) });

            return;
        }

        if (!data.has_update && !silent) {
            addSuccessToast(intl.getMessage('update_latest'));
        }
    } catch (error) {
        setState('checking', false);
        addErrorToast({ error });
    }
};

/** applyUpdate installs the latest release and waits for the restart. */
export const applyUpdate = async () => {
    setState('applying', true);

    try {
        await aghubUpdateApply();
    } catch (error) {
        setState('applying', false);
        addErrorToast({ error });

        return;
    }

    addSuccessToast(intl.getMessage('update_started'));

    await pollUpdateStatus();
};

/** getBackup loads the previous version a rollback would restore. */
export const getBackup = async () => {
    try {
        const data = await aghubUpdateBackup();
        setState('backup', data.backup ?? null);
    } catch (error) {
        addErrorToast({ error });
    }
};

/** rollbackUpdate restores the previous version and waits for the restart. */
export const rollbackUpdate = async () => {
    setState('rollingBack', true);

    try {
        await aghubUpdateRollback();
    } catch (error) {
        setState('rollingBack', false);
        addErrorToast({ error });

        return;
    }

    addSuccessToast(intl.getMessage('update_rollback_started'));

    await pollUpdateStatus();
};

/**
 * pollUpdateStatus polls the update state until the update finishes or the
 * server restarts and stops responding.
 */
const pollUpdateStatus = async () => {
    const deadline = Date.now() + pollTimeoutMs;

    while (Date.now() < deadline) {
        await new Promise((resolve) => {
            setTimeout(resolve, pollIntervalMs);
        });

        let data: UpdateStatusResponse;
        try {
            data = await aghubUpdateStatus();
        } catch {
            // The process is most likely restarting already.
            setState({ applying: false, rollingBack: false });

            return;
        }

        const status = data.status ?? emptyStatus;
        setState('status', status);

        if (status.error) {
            setState({ applying: false, rollingBack: false });
            addErrorToast({ error: new Error(status.error) });

            return;
        }

        if (!status.running) {
            setState({ applying: false, rollingBack: false });

            return;
        }
    }

    setState({ applying: false, rollingBack: false });
};
