import { createStore } from 'solid-js/store';
import { untrack } from 'solid-js';

import {
    portalFeedbackDelete,
    portalFeedbackList,
    portalFeedbackRead,
    portalFeedbackUpdate,
} from 'panel/api/generated';
import type { PortalFeedback } from 'panel/api/generated';

import { addErrorToast } from './toasts';

/** FeedbackFilter selects which messages a view shows. */
export type FeedbackFilter = 'all' | 'open' | 'resolved' | 'unread';

type FeedbackState = {
    items: PortalFeedback[];

    /** unread is the number of messages the administrator has not opened. */
    unread: number;

    /** open is the number of messages that are not resolved yet. */
    open: number;

    /** loading is true while the list is being fetched. */
    loading: boolean;

    /** processing is true while a change is being sent. */
    processing: boolean;
};

const initialState: FeedbackState = {
    items: [],
    unread: 0,
    open: 0,
    loading: false,
    processing: false,
};

const [state, setState] = createStore<FeedbackState>(initialState);

export const feedbackState = untrack(() => state);

/**
 * feedbackVisible returns the messages matching a filter.  "open" is about the
 * resolution and "unread" about the read flag, so the two are different sets.
 */
export const feedbackVisible = (
    items: PortalFeedback[],
    filter: FeedbackFilter,
): PortalFeedback[] => {
    switch (filter) {
        case 'open':
            return items.filter((f) => !f.resolved);
        case 'resolved':
            return items.filter((f) => !!f.resolved);
        case 'unread':
            return items.filter((f) => !f.read);
        default:
            return items;
    }
};

/**
 * adjustCounters moves the badges by the change to one message.  The list the
 * API returns is capped, so the totals cannot be recounted from the items held
 * here; the counters the server sends are the truth and only the delta is
 * applied to them.
 */
const adjustCounters = (before: PortalFeedback | undefined, after: PortalFeedback | undefined) => {
    const wasUnread = before ? !before.read : false;
    const wasOpen = before ? !before.resolved : false;
    const isUnread = after ? !after.read : false;
    const isOpen = after ? !after.resolved : false;

    setState({
        unread: Math.max(0, state.unread + Number(isUnread) - Number(wasUnread)),
        open: Math.max(0, state.open + Number(isOpen) - Number(wasOpen)),
    });
};

/** getFeedback loads the messages users left in the portal. */
export const getFeedback = async () => {
    setState('loading', true);

    try {
        const data = await portalFeedbackList();

        setState({
            items: data.items ?? [],
            unread: data.unread ?? 0,
            open: data.open ?? 0,
        });
    } catch (error) {
        addErrorToast({ error });
    } finally {
        setState('loading', false);
    }
};

/**
 * updateFeedback applies a partial change to one message and replaces it in the
 * state with the version the server returns.  An absent field is left as it is,
 * so closing a message does not wipe its reply.
 */
export const updateFeedback = async (
    id: string,
    patch: { resolved?: boolean; private?: boolean; reply?: string },
) => {
    setState('processing', true);

    try {
        const before = state.items.find((f) => f.id === id);
        const data = await portalFeedbackUpdate({ id, ...patch });

        if (data.item) {
            const updated = data.item;

            setState('items', (prev) => prev.map((f) => (f.id === id ? updated : f)));
            adjustCounters(before, updated);
        }
    } catch (error) {
        addErrorToast({ error });
    } finally {
        setState('processing', false);
    }
};

/** markAllRead marks every message read without hiding it. */
export const markAllRead = async () => {
    setState('processing', true);

    try {
        await portalFeedbackRead();

        setState('items', (prev) => prev.map((f) => ({ ...f, read: true })));
        setState('unread', 0);
    } catch (error) {
        addErrorToast({ error });
    } finally {
        setState('processing', false);
    }
};

/** removeFeedback deletes one message. */
export const removeFeedback = async (id: string) => {
    setState('processing', true);

    try {
        const before = state.items.find((f) => f.id === id);

        await portalFeedbackDelete({ id });

        setState('items', (prev) => prev.filter((f) => f.id !== id));
        adjustCounters(before, undefined);
    } catch (error) {
        addErrorToast({ error });
    } finally {
        setState('processing', false);
    }
};
