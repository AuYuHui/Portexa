export interface ClosedOrder {
  order_ticket?: string;
  deal_ticket?: string;
  side: "buy" | "sell";
  symbol: string;
  lots: string;
  close_time_ms: number | null;
  close_time_wall_ms: number;
  open_time_wall: number;
  open_price: string | null;
  close_price: string;
  digits: number;
  profit: string;
  commission: string;
  swap: string;
  taxes: string;
  fee: string | null;
  net_profit: string;
}

export interface ClosedOrderPage {
  items: ClosedOrder[];
  total: number;
  page: number;
  page_size: number;
  currency: string;
  net_profit: string | null;
  known_net_profit: string;
  complete: boolean;
  as_of_ms: number | null;
}
