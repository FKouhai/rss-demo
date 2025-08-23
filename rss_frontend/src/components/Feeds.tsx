import type { FunctionalComponent } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
const Feeds = ({ pollerEndpoint }) => {
  const [data, setData] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [page, setPage] = useState(1);
  const [expandedItems, setExpandedItems] = useState({});
  const ITEMS_PER_PAGE = 9;

  const truncateHtml = (html, maxLength) => {
    const plainText = html.replace(/<[^>]*>?/gm, '');
    if (plainText.length <= maxLength) {
      return html;
    }
    const truncatedText = plainText.substring(0, maxLength);
    return `${truncatedText}...`;
  };

  useEffect(() => {
    const fetchData = async () => {
      setLoading(true);
      setError(null);
      
      if (!pollerEndpoint) {
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
      } catch (err) {
        setError("Failed to fetch data.");
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

  if (loading) return <div className="p-4 text-center text-lg">Loading...</div>;
  if (error) return <div className="p-4 text-center text-red-500">Error: {error}</div>;

  return (
    <div className="p-4 min-h-screen bg-gray-50 flex flex-col justify-between">
      <div className="container mx-auto">
        <h1 className="text-3xl md:text-4xl font-bold text-center mb-8">Latest Feeds</h1>
        <div className="grid grid-cols-1 sm:grid-cols-3 lg:grid-cols-3 gap-6">
          {paginatedData.map((item, index) => {
            const isExpanded = expandedItems[index] || false;
            const content = item.content;
            const isLongContent = content.length > 300;
            const displayContent = isLongContent && !isExpanded ? truncateHtml(content, 300) : content;

            return (
              <Card key={index} className="flex flex-col">
                <CardHeader>
                  <CardTitle>{item.title}</CardTitle>
                  <CardDescription>
                    <a href={item.link} className="text-blue-500 hover:text-blue-700 text-sm">View Original Post</a>
                  </CardDescription>
                </CardHeader>
                <CardContent className="flex-grow">
                  <div
                    className="feeds-content text-sm text-gray-600 mb-9"
                    dangerouslySetInnerHTML={{ __html: displayContent }}
                  />
                  {isLongContent && (
                    <Button
                      onClick={() => setExpandedItems({ ...expandedItems, [index]: !isExpanded })}
                      className="mt-4"
                    >
                      {isExpanded ? 'Read Less' : 'Read More'}
                    </Button>
                  )}
                </CardContent>
              </Card>
            );
          })}
        </div>
      </div>
      <div className="container mx-auto mt-8 flex justify-center items-center space-x-4">
        <Button onClick={handlePrevPage} disabled={page === 1}>Previous</Button>
        <span className="text-gray-700">Page {page}</span>
        <Button onClick={handleNextPage} disabled={!hasMorePages}>Next</Button>
      </div>
    </div>
  );
};

export default Feeds;
