import type { FunctionalComponent } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"

interface RssObject {
  title: string;
  content: string;
  link: string;
}

interface FeedsProps {
  pollerEndpoint: string;
}

const ITEMS_PER_PAGE = 9; // Set the number of items you want per page

const Feeds: FunctionalComponent<FeedsProps> = ({ pollerEndpoint }) => {
  const [data, setData] = useState<RssObject[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [expandedItems, setExpandedItems] = useState<{ [key: number]: boolean }>({});

  const truncateHtml = (html: string, maxLength: number) => {
    const plainText = html.replace(/<[^>]*>?/gm, '');
    if (plainText.length <= maxLength) {
      return html;
    }
    const truncatedText = plainText.substring(0, maxLength);
    return `${truncatedText}...`;
  };

  useEffect(() => {
    const fetchData = async () => {
      if (!pollerEndpoint) {
        console.error('The pollerEndpoint prop is not set.');
        setError('Service endpoint not configured.');
        setLoading(false);
        return;
      }

      try {
        const response = await fetch(pollerEndpoint);
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`);
        }
        const jsonData = await response.json();
        setData(jsonData);
      } catch (err: any) {
        setError(err.message);
      } finally {
        setLoading(false);
      }
    };

    fetchData();
  }, [pollerEndpoint]);

  const handleNextPage = () => {
    setPage(prevPage => prevPage + 1);
  };

  const handlePrevPage = () => {
    setPage(prevPage => Math.max(1, prevPage - 1));
  };

  const startIndex = (page - 1) * ITEMS_PER_PAGE;
  const endIndex = startIndex + ITEMS_PER_PAGE;
  const paginatedData = data.slice(startIndex, endIndex);

  const hasMorePages = endIndex < data.length;

  if (loading) return <div>Loading...</div>;
  if (error) return <div>Error: {error}</div>;

  return (
    <div className="feeds-container">

      <div className="feeds-grid">
        {paginatedData.map((item, index) => {
          const isExpanded = expandedItems[index] || false;
          const content = item.content;
          const isLongContent = content.length > 200;
          const displayContent = isLongContent && !isExpanded ? truncateHtml(content, 200) : content;

          return (
            <div key={index} className="feeds-item-box">
              <Card>
              <CardHeader>
                <CardTitle>{item.title}</CardTitle>
                <CardDescription></CardDescription>
                <CardAction><a href={item.link}>View the Original Post</a></CardAction>
              </CardHeader>
              <CardContent>
              <div
                className="feeds-content"
                dangerouslySetInnerHTML={{ __html: displayContent }}
              />
              </CardContent>
            </Card>
              {isLongContent && (
                <Button
                  className="read-more-btn"
                  onClick={() => setExpandedItems({ ...expandedItems, [index]: !isExpanded })}
                >
                  {isExpanded ? 'Read Less' : 'Read More'}
                </Button>
              )}
            </div>
          );
        })}
      </div>
      <div className="pagination">
        <Button onClick={handlePrevPage} disabled={page === 1}>Previous</Button>
        <span>Page {page}</span>
        <Button onClick={handleNextPage} disabled={!hasMorePages}>Next</Button>
      </div>
    </div>
  );
};

export default Feeds;
