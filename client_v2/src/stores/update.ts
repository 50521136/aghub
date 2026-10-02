import { createStore } from 'solid-js/store';
import { untrack } from 'solid-js';

import {
    aghubUpdateApply,
    aghubUpdateCheck,
    aghubUpdateStatus,
} from 'panel/api/generated';
import type { UpdateCheckResponse, UpdateStatus, UpdateStatusResponse } from 'panel/api/model';

import { addErrorToast, addSuccessToast } from './toasts';
import intl from 'panel/common/intl';

/** pollIntervalMs is the interval of the update state polling. */
const pollIntervalMs = 2000;

/** pollTimeoutMs is how long the polling continues before giving up. */
const pollTimeoutMs = 5 * 60 * 1000;

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

/** checkForUpdate queries GitHub for a newer release. */
export const checkForUpdate = async () => {
    setState('checking', true);

    try {
        const data = await aghubUpdateCheck();
        setState({ check: data, checking: false });

        if (data.error) {
            addErrorToast({ error: new Error(data.error) });

            return;
        }

        if (!data.has_update) {
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
            setState({ applying: false });

            return;
        }

        const status = data.status ?? emptyStatus;
        setState('status', status);

        if (status.error) {
            setState('applying', false);
            addErrorToast({ error: new Error(status.error) });

            return;
        }

        if (!status.running) {
            setState('applying', false);

            return;
        }
    }

    setState('applying', false);
};
