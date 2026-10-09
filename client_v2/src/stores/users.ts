import { createStore } from 'solid-js/store';
import { untrack } from 'solid-js';
import {
    usersAdd,
    usersBackups,
    usersBulkAdd,
    usersDelete,
    usersList,
    portalPackage,
    usersPassword,
    usersReset,
    usersRestore,
    usersSetSettings,
    usersToggle,
    usersUpdate,
} from 'panel/api/generated';
import type {
    BackupInfo,
    User,
    UserAddRequest,
    UserBulkAddResponse,
    UserSettings,
    UserUpdateRequest,
    UsersSummary,
} from 'panel/api/model';

import { customFetch } from 'panel/api/customFetch';
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
    /** offline is true when the list on screen came from the local cache
     *  because the panel could not reach the server. */
    offline: boolean;
    /** cachedAt is when that cached list was taken, in Unix milliseconds.  It
     *  is zero when the list came from the server. */
    cachedAt: number;
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
    offline: false,
    cachedAt: 0,
};

const [state, setState] = createStore<UsersState>(initialState);

export const usersState = untrack(() => state);

/** CACHE_KEY is the key of the cached user list in the local storage. */
const CACHE_KEY = 'aghub.users.cache';

/**
 * CachedUsers is the last user list this browser fetched successfully.  It is
 * what the panel shows when the server cannot be reached, so that a restart or
 * a broken network does not turn the page into an error screen.
 *
 * It holds the list only.  The settings are deliberately left out: they carry
 * the portal deployment token, and a token in the browser storage is readable
 * by anything that can run a script on the page.
 */
type CachedUsers = {
    /** at is when the cache was taken, in Unix milliseconds. */
    at: number;
    users: User[];
    summary: UsersSummary;
    domain: string;
};

/**
 * readCache returns the cached user list, or null when there is none or it
 * cannot be parsed.  A cache that cannot be read is not an error: the panel
 * simply has nothing to show offline.
 */
const readCache = (): CachedUsers | null => {
    try {
        const raw = localStorage.getItem(CACHE_KEY);
        if (!raw) {
            return null;
        }

        const parsed = JSON.parse(raw) as CachedUsers;
        if (!parsed || !Array.isArray(parsed.users)) {
            return null;
        }

        return parsed;
    } catch {
        return null;
    }
};

/**
 * writeCache stores the list for the offline case.  A full or unavailable
 * local storage must not break the panel, so a failure here is ignored.
 */
const writeCache = (data: Omit<CachedUsers, 'at'>) => {
    try {
        localStorage.setItem(CACHE_KEY, JSON.stringify({ at: Date.now(), ...data }));
    } catch {
        // Ignored on purpose.
    }
};

export const getUsers = async () => {
    setState('processing', true);

    try {
        // The settings come back with the list so that the page needs a single
        // request; the write side is a POST to its own path.
        const data = await usersList();

        const users = data.users || [];
        const summary = data.summary || emptySummary;
        const domain = data.domain || '';

        setState({
            users,
            summary,
            settings: data.settings || emptySettings,
            domain,
            initialized: true,
            processing: false,
            offline: false,
            cachedAt: 0,
        });

        writeCache({ users, summary, domain });
    } catch (error) {
        setState('processing', false);

        // The server is unreachable.  The last list this browser fetched is
        // still worth showing as long as it is marked as stale, so the panel
        // says "this is what we knew at 14:20" instead of showing nothing.
        //
        // The cache is only ever rendered.  It is never sent back: it is older
        // than the server by definition, so writing it back would silently
        // roll back every change made since it was taken.
        const cached = readCache();
        if (cached) {
            setState({
                users: cached.users,
                summary: cached.summary || emptySummary,
                domain: cached.domain || '',
                initialized: true,
                offline: true,
                cachedAt: cached.at,
            });
        }

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

// rotatePortalToken replaces the portal deployment token.  The front-end built
// from the previous token stops working, which is how a leaked deployment
// package is cut off.
export const rotatePortalToken = async (): Promise<string> => {
    setState('processingSave', true);

    try {
        const data = await customFetch<{ token: string }>('control/users/portal-token', {
            method: 'POST',
        });

        await getUsers();
        setState('processingSave', false);
        addSuccessToast(intl.getMessage('portal_token_rotated'));

        return data.token || '';
    } catch (error) {
        addErrorToast({ error });
        setState('processingSave', false);

        return '';
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

/**
 * setUserPassword sets the password the user signs in to the portal with.  An
 * empty password revokes the portal access of the user.
 */
export const setUserPassword = async (uid: string, password: string): Promise<boolean> => {
    setState('processingSave', true);

    let ok = false;
    try {
        await usersPassword({ uid, password });
        addSuccessToast(intl.getMessage(
            password ? 'users_portal_password_set' : 'users_portal_password_cleared',
        ));
        ok = true;
    } catch (error) {
        addErrorToast({ error });
    }

    setState('processingSave', false);
    await getUsers();

    return ok;
};

/**
 * downloadPortalPackage saves the deployment package of the portal front-end.
 *
 * The response is a zip, so it is not parsed; the browser is handed a temporary
 * object URL and downloads it.  The URL is revoked right away because the
 * download has already started by then.
 */
export const downloadPortalPackage = async (): Promise<void> => {
    try {
        const blob = await portalPackage();
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = 'aghub-portal.zip';
        document.body.appendChild(a);
        a.click();
        a.remove();
        URL.revokeObjectURL(url);
        addSuccessToast(intl.getMessage('users_portal_package_downloaded'));
    } catch (error) {
        addErrorToast({ error });
    }
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

/**
 * getBackups loads the dated backups of the user state.
 */
export const getBackups = async (): Promise<BackupInfo[]> => {
    try {
        const data = await usersBackups();

        return data.backups || [];
    } catch (error) {
        addErrorToast({ error });

        return [];
    }
};

/**
 * restoreBackup replaces all users with the ones from the named backup and
 * reloads the list.
 */
export const restoreBackup = async (name: string): Promise<boolean> => {
    setState('processingSave', true);

    try {
        const data = await usersRestore({ name });

        addSuccessToast(
            intl.getMessage('users_backup_restored', { count: data.users ?? 0 }),
        );

        setState('processingSave', false);
        await getUsers();

        return true;
    } catch (error) {
        addErrorToast({ error });

        setState('processingSave', false);

        return false;
    }
};
