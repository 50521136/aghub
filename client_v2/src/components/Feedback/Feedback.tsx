import { createMemo, createSignal, For, onMount, Show } from 'solid-js';
import { createStore } from 'solid-js/store';
import cn from 'clsx';

import intl from 'panel/common/intl';
import theme from 'panel/lib/theme';
import { Button } from 'panel/common/ui/Button';
import { ConfirmDialog } from 'panel/common/ui/ConfirmDialog';
import { Textarea } from 'panel/common/controls/Textarea';
import type { PortalFeedback } from 'panel/api/generated';
import {
    feedbackState,
    feedbackVisible,
    getFeedback,
    markAllRead,
    removeFeedback,
    updateFeedback,
} from 'panel/stores/feedback';
import type { FeedbackFilter } from 'panel/stores/feedback';

import s from './Feedback.module.pcss';

/** The filters of the toolbar, in display order. */
const FILTERS: FeedbackFilter[] = ['all', 'open', 'resolved', 'unread'];

/** filterLabel returns the localized label of a filter. */
const filterLabel = (filter: FeedbackFilter): string => {
    switch (filter) {
        case 'open':
            return intl.getMessage('feedback_filter_open');
        case 'resolved':
            return intl.getMessage('feedback_filter_resolved');
        case 'unread':
            return intl.getMessage('feedback_filter_unread');
        default:
            return intl.getMessage('feedback_filter_all');
    }
};

/** formatTime renders a Unix timestamp in seconds as a local date and time. */
const formatTime = (seconds: number): string => {
    if (!seconds) {
        return '';
    }

    return new Date(seconds * 1000).toLocaleString();
};

export const Feedback = () => {
    const [filter, setFilter] = createSignal<FeedbackFilter>('all');
    // The reply boxes are editable before they are saved, so their text lives
    // here, keyed by message id, and falls back to the saved reply.
    const [drafts, setDrafts] = createStore<Record<string, string>>({});
    const [confirmDelete, setConfirmDelete] = createSignal('');

    onMount(() => {
        void getFeedback();
    });

    const items = createMemo(() => feedbackVisible(feedbackState.items, filter()));

    const resolvedCount = createMemo(() => feedbackState.items.filter((f) => f.resolved).length);

    /** replyDraft is the text in a message's reply box. */
    const replyDraft = (f: PortalFeedback) => drafts[f.id] ?? f.reply ?? '';

    const doDelete = () => {
        const id = confirmDelete();

        if (!id) {
            return;
        }

        void removeFeedback(id);
        setConfirmDelete('');
    };

    return (
        <div class={s.page}>
            <h1 class={cn(theme.title.h4, s.title)}>{intl.getMessage('feedback_title')}</h1>
            <p class={cn(theme.text.t3, s.hint)}>{intl.getMessage('feedback_hint')}</p>

            <div class={s.stats}>
                <div class={s.statCard}>
                    <span class={cn(theme.title.h5, s.statValue)}>{feedbackState.unread}</span>
                    <span class={cn(theme.text.t4, s.statLabel)}>
                        {intl.getMessage('feedback_stat_unread')}
                    </span>
                </div>
                <div class={s.statCard}>
                    <span class={cn(theme.title.h5, s.statValue)}>{feedbackState.open}</span>
                    <span class={cn(theme.text.t4, s.statLabel)}>
                        {intl.getMessage('feedback_stat_open')}
                    </span>
                </div>
                <div class={s.statCard}>
                    <span class={cn(theme.title.h5, s.statValue)}>{resolvedCount()}</span>
                    <span class={cn(theme.text.t4, s.statLabel)}>
                        {intl.getMessage('feedback_stat_resolved')}
                    </span>
                </div>
                <div class={s.statCard}>
                    <span class={cn(theme.title.h5, s.statValue)}>
                        {intl.getMessage('feedback_stat_total', {
                            count: String(feedbackState.items.length),
                        })}
                    </span>
                </div>
            </div>

            <div class={s.toolbar}>
                <div class={s.chips}>
                    <For each={FILTERS}>
                        {(f) => (
                            <button
                                type="button"
                                class={cn(s.chip, { [s.chipActive]: filter() === f })}
                                onClick={() => setFilter(f)}
                                data-testid={`feedback-filter-${f}`}
                            >
                                {filterLabel(f)}
                            </button>
                        )}
                    </For>
                </div>

                <div class={s.toolbarActions}>
                    <Button
                        variant="secondary"
                        disabled={feedbackState.unread === 0 || feedbackState.processing}
                        onClick={() => void markAllRead()}
                        data-testid="feedback-mark-read"
                    >
                        {intl.getMessage('feedback_mark_read')}
                    </Button>
                    <Button
                        variant="secondary"
                        disabled={feedbackState.loading}
                        onClick={() => void getFeedback()}
                        data-testid="feedback-reload"
                    >
                        {intl.getMessage('feedback_reload')}
                    </Button>
                </div>
            </div>

            <Show
                when={items().length > 0}
                fallback={
                    <p class={cn(theme.text.t3, s.empty)}>
                        {intl.getMessage('feedback_empty')}
                    </p>
                }
            >
                <ul class={s.list}>
                    <For each={items()}>
                        {(f) => (
                            <li
                                class={cn(s.item, { [s.itemUnread]: !f.read })}
                                data-testid={`feedback-item-${f.id}`}
                            >
                                <div class={s.itemHead}>
                                    <div class={s.who}>
                                        <span
                                            class={cn(
                                                theme.text.t3,
                                                theme.text.medium,
                                                s.name,
                                            )}
                                        >
                                            {f.name || f.uid || intl.getMessage('feedback_anonymous')}
                                        </span>
                                        <Show when={f.uid}>
                                            <span class={cn(theme.text.t4, s.uid)}>{f.uid}</span>
                                        </Show>
                                    </div>
                                    <span class={cn(theme.text.t4, s.time)}>
                                        {formatTime(f.created_at)}
                                    </span>
                                </div>

                                <div class={s.pills}>
                                    <Show when={!f.read}>
                                        <span class={cn(theme.text.t4, s.pill, s.pillUnread)}>
                                            {intl.getMessage('feedback_status_unread')}
                                        </span>
                                    </Show>
                                    <Show when={f.resolved}>
                                        <span class={cn(theme.text.t4, s.pill, s.pillResolved)}>
                                            {intl.getMessage('feedback_status_resolved')}
                                        </span>
                                    </Show>
                                    <span
                                        class={cn(theme.text.t4, s.pill, {
                                            [s.pillPrivate]: f.private,
                                        })}
                                    >
                                        {intl.getMessage(
                                            f.private
                                                ? 'feedback_status_private'
                                                : 'feedback_status_public',
                                        )}
                                    </span>
                                </div>

                                <Show when={f.contact}>
                                    <span class={cn(theme.text.t4, s.contact)}>{f.contact}</span>
                                </Show>

                                <p class={cn(theme.text.t3, s.content)}>{f.content}</p>

                                <div class={s.reply}>
                                    <Textarea
                                        value={replyDraft(f)}
                                        placeholder={intl.getMessage('feedback_reply_placeholder')}
                                        onInput={(e) => setDrafts(f.id, e.currentTarget.value)}
                                        data-testid={`feedback-reply-${f.id}`}
                                    />
                                    <Button
                                        variant="secondary"
                                        disabled={feedbackState.processing}
                                        onClick={() => void updateFeedback(f.id, { reply: replyDraft(f) })}
                                        data-testid={`feedback-save-${f.id}`}
                                    >
                                        {intl.getMessage('feedback_save_reply')}
                                    </Button>
                                </div>

                                <div class={s.actions}>
                                    <Button
                                        variant="secondary"
                                        disabled={feedbackState.processing}
                                        onClick={() =>
                                            void updateFeedback(f.id, { resolved: !f.resolved })
                                        }
                                        data-testid={`feedback-resolve-${f.id}`}
                                    >
                                        {intl.getMessage(
                                            f.resolved ? 'feedback_reopen' : 'feedback_resolve',
                                        )}
                                    </Button>
                                    <Button
                                        variant="secondary"
                                        disabled={feedbackState.processing}
                                        onClick={() =>
                                            void updateFeedback(f.id, { private: !f.private })
                                        }
                                        data-testid={`feedback-visibility-${f.id}`}
                                    >
                                        {intl.getMessage(
                                            f.private
                                                ? 'feedback_make_public'
                                                : 'feedback_make_private',
                                        )}
                                    </Button>
                                    <Button
                                        variant="secondary-danger"
                                        disabled={feedbackState.processing}
                                        onClick={() => setConfirmDelete(f.id)}
                                        data-testid={`feedback-delete-${f.id}`}
                                    >
                                        {intl.getMessage('feedback_delete')}
                                    </Button>
                                </div>
                            </li>
                        )}
                    </For>
                </ul>
            </Show>

            <Show when={confirmDelete()}>
                <ConfirmDialog
                    onClose={() => setConfirmDelete('')}
                    onConfirm={doDelete}
                    submitDisabled={feedbackState.processing}
                    buttonText={intl.getMessage('yes_remove')}
                    cancelText={intl.getMessage('cancel')}
                    buttonVariant="danger"
                    title={intl.getMessage('feedback_delete_title')}
                    text={intl.getMessage('feedback_delete_desc')}
                    submitTestId="feedback-delete-confirm"
                />
            </Show>
        </div>
    );
};
