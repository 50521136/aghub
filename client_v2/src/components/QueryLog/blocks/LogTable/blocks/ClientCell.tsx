import { Show } from 'solid-js';
import cn from 'clsx';

import { Icon } from 'panel/common/ui/Icon';
import theme from 'panel/lib/theme';
import { agHubUserFor, getClientLocation } from 'panel/components/QueryLog/helpers';
import type { NormalizedQueryLogItem } from 'panel/helpers/helpers';

import s from '../LogTable.module.pcss';

type Props = {
    onSearchSelect: (value: string) => (event: MouseEvent) => void;
    row: NormalizedQueryLogItem;
};

export const ClientCell = (props: Props) => {
    const agHubUser = () => agHubUserFor(props.row.client_id);
    // The name AdGuard Home shows comes from reverse DNS, which for a home or
    // mobile connection is often junk — a carrier IP whose PTR record says
    // "localhost" — so the user name wins whenever the identifier is ours.
    const clientName = () => (agHubUser() ? '' : props.row.client_info?.name || '');
    const clientLocation = () => getClientLocation(props.row.client_info?.whois);
    const secondary = () => (agHubUser() ? props.row.client_id : clientName());

    // Quoting the term switches AdGuard Home's search to strict matching, which
    // compares the client ID exactly.  Without the quotes it also matches any
    // domain that merely contains the same letters, and a short identifier such
    // as "a" would come back buried in unrelated rows.
    const exactSearch = (value: string) => `"${value}"`;

    return (
        <div class={s.clientCell} data-testid="query-log-client-cell">
            <div class={s.clientPrimary}>
                <button
                    type="button"
                    class={cn(s.clientButtonPlain, s.clientIp, theme.text.t3)}
                    title={props.row.client}
                    onClick={(e) => {
                        e.stopPropagation();
                        props.onSearchSelect(props.row.client)(e);
                    }}
                >
                    {props.row.client}
                </button>

                <Show when={agHubUser()}>
                    <button
                        type="button"
                        class={cn(s.clientButtonPlain, s.clientUser)}
                        title={props.row.client_id}
                        onClick={(e) => {
                            e.stopPropagation();
                            props.onSearchSelect(exactSearch(props.row.client_id!))(e);
                        }}
                    >
                        {agHubUser()}
                    </button>
                </Show>
            </div>
            <div class={s.clientSecondary}>
                <Show when={secondary()}>
                    <button
                        type="button"
                        class={cn(s.clientButtonPlain, s.clientName, theme.text.t4)}
                        title={secondary()}
                        onClick={(e) =>
                            props.onSearchSelect(
                                agHubUser() ? exactSearch(secondary()) : secondary(),
                            )(e)
                        }
                    >
                        {secondary()}
                    </button>
                </Show>

                <Show when={secondary() && clientLocation()}>
                    <span class={s.clientLocationDivider} />
                </Show>

                <Show when={clientLocation()}>
                    <span class={s.clientLocation} title={clientLocation()}>
                        <Icon icon="location" class={s.clientLocationIcon} />
                        <span class={cn(s.clientLocationText, theme.text.t4)}>
                            {clientLocation()}
                        </span>
                    </span>
                </Show>
            </div>
        </div>
    );
};
