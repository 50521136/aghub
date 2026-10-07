import { Show } from 'solid-js';
import cn from 'clsx';

import intl from 'panel/common/intl';
import theme from 'panel/lib/theme';
import { Button } from 'panel/common/ui/Button';
import { Dialog } from 'panel/common/ui/Dialog';
import { Textarea } from 'panel/common/controls/Textarea';
import type { PortalFeedback } from 'panel/api/generated';

import s from './FeedbackDialog.module.pcss';

type Props = {
    /** item is the message the dialog is showing. */
    item: PortalFeedback;

    /** draft is the current text of the reply box. */
    draft: string;

    /** processing is true while a change is being sent. */
    processing: boolean;

    onDraft: (value: string) => void;
    onSaveReply: () => void;
    onResolve: () => void;
    onToggleVisibility: () => void;
    onDelete: () => void;
    onClose: () => void;
};

/** pad left-pads a number to two digits. */
const pad = (n: number): string => String(n).padStart(2, '0');

/** formatTime renders a Unix timestamp in seconds as a compact local date and time. */
const formatTime = (seconds: number): string => {
    if (!seconds) {
        return '';
    }

    const d = new Date(seconds * 1000);

    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(
        d.getHours(),
    )}:${pad(d.getMinutes())}`;
};

export const FeedbackDialog = (props: Props) => {
    const userName = () =>
        props.item.name || props.item.uid || intl.getMessage('feedback_anonymous');

    return (
        <Dialog
            visible
            onClose={props.onClose}
            title={
                <span data-testid="feedback-detail-title">
                    {intl.getMessage('feedback_detail_title')}
                </span>
            }
            class={s.dialog}
        >
            <div class={s.form} data-testid="feedback-detail-modal">
                <div class={s.field}>
                    <span class={s.fieldLabel}>{intl.getMessage('feedback_col_status')}</span>
                    <div class={s.badges}>
                        <Show when={!props.item.read}>
                            <span class={cn(s.badge, s.badgeUnread)}>
                                {intl.getMessage('feedback_status_unread')}
                            </span>
                        </Show>
                        <Show when={props.item.resolved}>
                            <span class={cn(s.badge, s.badgeResolved)}>
                                {intl.getMessage('feedback_status_resolved')}
                            </span>
                        </Show>
                        <span class={cn(s.badge, { [s.badgePrivate]: props.item.private })}>
                            {intl.getMessage(
                                props.item.private
                                    ? 'feedback_status_private'
                                    : 'feedback_status_public',
                            )}
                        </span>
                    </div>
                </div>

                <div class={s.field}>
                    <span class={s.fieldLabel}>{intl.getMessage('feedback_col_user')}</span>
                    <div class={s.userBox}>
                        <span class={s.userName}>{userName()}</span>
                        <Show when={props.item.uid && props.item.name}>
                            <span class={s.userUid}>{props.item.uid}</span>
                        </Show>
                    </div>
                </div>

                <Show when={props.item.contact}>
                    <div class={s.field}>
                        <span class={s.fieldLabel}>
                            {intl.getMessage('feedback_field_contact')}
                        </span>
                        <span class={cn(s.fieldValue, s.breakAll)}>{props.item.contact}</span>
                    </div>
                </Show>

                <div class={s.field}>
                    <span class={s.fieldLabel}>{intl.getMessage('feedback_col_time')}</span>
                    <span class={s.fieldValue}>{formatTime(props.item.created_at)}</span>
                </div>

                <div class={s.field}>
                    <span class={s.fieldLabel}>{intl.getMessage('feedback_col_content')}</span>
                    <p class={cn(s.fieldValue, s.message)}>{props.item.content}</p>
                </div>

                <div class={s.replyField}>
                    <label class={s.fieldLabel} for="feedback-reply">
                        {intl.getMessage('feedback_field_reply')}
                    </label>
                    <Textarea
                        id="feedback-reply"
                        rows={3}
                        value={props.draft}
                        placeholder={intl.getMessage('feedback_reply_placeholder')}
                        onInput={(e) => props.onDraft(e.currentTarget.value)}
                        data-testid={`feedback-reply-${props.item.id}`}
                    />
                    <div class={s.replyActions}>
                        <Button
                            size="small"
                            onClick={props.onSaveReply}
                            disabled={props.processing}
                            data-testid={`feedback-save-${props.item.id}`}
                        >
                            {intl.getMessage('feedback_save_reply')}
                        </Button>
                    </div>
                </div>
            </div>

            {/* The footer is a sibling of the padded form so that the shared
                dialog footer keeps its own 16px inset, which lines the buttons
                up with the title instead of nesting one inset inside another. */}
            <div class={cn(theme.dialog.footer, s.footer)}>
                <Button
                    size="small"
                    variant="secondary"
                    class={theme.dialog.button}
                    onClick={props.onResolve}
                    disabled={props.processing}
                    data-testid={`feedback-detail-resolve-${props.item.id}`}
                >
                    {intl.getMessage(props.item.resolved ? 'feedback_reopen' : 'feedback_resolve')}
                </Button>

                <Button
                    size="small"
                    variant="secondary"
                    class={theme.dialog.button}
                    onClick={props.onToggleVisibility}
                    disabled={props.processing}
                    data-testid={`feedback-detail-visibility-${props.item.id}`}
                >
                    {intl.getMessage(
                        props.item.private ? 'feedback_make_public' : 'feedback_make_private',
                    )}
                </Button>

                <Button
                    size="small"
                    variant="danger"
                    class={theme.dialog.button}
                    onClick={props.onDelete}
                    disabled={props.processing}
                    data-testid={`feedback-detail-delete-${props.item.id}`}
                >
                    {intl.getMessage('feedback_delete')}
                </Button>
            </div>
        </Dialog>
    );
};
