import { createStore } from 'solid-js/store';
import { untrack } from 'solid-js';
import {
    usersAdd,
    usersBulkAdd,
    usersDelete,
    usersList,
    usersReset,
    usersSetSettings,
    usersToggle,
    usersUpdate,
} from 'panel/api/generated';
import type {
    User,
    UserAddRequest,
    UserBulkAddResponse,
    UserSettings,
    UserUpdateRequest,
    UsersSummary,
} from 'panel/api/model';

import intl from 'panel/common/intl';
import { addErrorToast, addSuccessToast } from './toasts';

type UsersState = {
    users: User[];
    summary: UsersSummary;
    settings: UserSettings;
    /** domain is the DoT/DoH domain an identifier is joined to. */
    domain: string;
    initialized: boolean;
    processing: boolean;
    processingSave: boolean;
};

const emptySummary: UsersSummary = {
    total: 0,
    active: 0,
    disabled: 0,
    expired: 0,
    over_quota: 0,
    expiring_soon: 0,
    requests: 0,
    total_requests: 0,
};

const emptySettings: UserSettings = {
    deny_unmatched: false,
};

const initialState: UsersState = {
    users: [],
    summary: emptySummary,
    settings: emptySettings,
    domain: '',
    initialized: false,
    processing: true,
    processingSave: false,
};

const [state, setState] = createStore<UsersState>(initialState);

export const usersState = untrack(() => state);

export const getUsers = async () => {
    setState('processing', true);

    try {
        // The settings come back with the list so that the page needs a single
        // request; the write side is a POST to its own path.
        const data = await usersList();

        setState({
            users: data.users || [],
            summary: data.summary || emptySummary,
            settings: data.settings || emptySettings,
            domain: data.domain || '',
            initialized: true,
            processing: false,
        });
    } catch (error) {
        setState('processing', false);
        addErrorToast({ error });
    }
};

export const saveSettings = async (settings: UserSettings): Promise<boolean> => {
    setState('processingSave', true);

    try {
        const data = await usersSetSettings(settings);
        setState('settings', data.settings || emptySettings);
        setState('processingSave', false);
        addSuccessToast(intl.getMessage('users_settings_saved'));

        return true;
    } catch (error) {
        addErrorToast({ error });
        setState('processingSave', false);

        return false;
    }
};

// bulkAddUsers creates one user per line of the payload.  The API answers with
// 200 even when the payload has problems, so the per-line errors come back in
// the result rather than as a thrown error.
export const bulkAddUsers = async (text: string, enabled: boolean) => {
    setState('processingSave', true);

    let result: UserBulkAddResponse | undefined;
    let failed = false;

    try {
        result = await usersBulkAdd({ text, enabled });
    } catch (error) {
        addErrorToast({ error });
        failed = true;
    }

    setState('processingSave', false);

    if (failed) {
        return undefined;
    }

    if (!result || (result.errors && result.errors.length > 0)) {
        return result;
    }

    addSuccessToast(
        intl.getMessage('users_bulk_added', {
            count: String(result.users?.length || 0),
        }),
    );
    await getUsers();

    return result;
};

export const addUser = async (config: UserAddRequest): Promise<boolean> => {
    setState('processingSave', true);

    try {
        await usersAdd(config);
        setState('processingSave', false);
        addSuccessToast(intl.getMessage('users_added'));
        await getUsers();

        return true;
    } catch (error) {
        addErrorToast({ error });
        setState('processingSave', false);

        return false;
    }
};

export const updateUser = async (config: UserUpdateRequest): Promise<boolean> => {
    setState('processingSave', true);

    try {
        await usersUpdate(config);
        setState('processingSave', false);
        addSuccessToast(intl.getMessage('users_saved'));
        await getUsers();

        return true;
    } catch (error) {
        addErrorToast({ error });
        setState('processingSave', false);

        return false;
    }
};

export const deleteUsers = async (uids: string[]) => {
    setState('processingSave', true);

    try {
        await usersDelete({ uids });
        addSuccessToast(intl.getMessage('users_removed'));
    } catch (error) {
        addErrorToast({ error });
    }

    setState('processingSave', false);
    await getUsers();
};

export const resetUsers = async (uids: string[]) => {
    setState('processingSave', true);

    try {
        await usersReset({ uids });
        addSuccessToast(intl.getMessage('users_reset_done'));
    } catch (error) {
        addErrorToast({ error });
    }

    setState('processingSave', false);
    await getUsers();
};

export const toggleUsers = async (uids: string[], enabled: boolean) => {
    setState('processingSave', true);

    try {
        await usersToggle({ uids, enabled });
        addSuccessToast(intl.getMessage(enabled ? 'users_enabled_toast' : 'users_disabled_toast'));
    } catch (error) {
        addErrorToast({ error });
    }

    setState('processingSave', false);
    await getUsers();
};
